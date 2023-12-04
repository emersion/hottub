package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"git.sr.ht/~emersion/go-oauth2"
	"git.sr.ht/~emersion/gqlclient"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/go-github/v56/github"
	"gopkg.in/yaml.v3"

	"git.sr.ht/~emersion/hottub/buildssrht"
)

const (
	monitorJobInterval   = 5 * time.Second
	monitorMaxRetries    = 10
	srhtGrants           = "builds.sr.ht/PROFILE:RO builds.sr.ht/JOBS:RW"
	maxJobsPerCheckSuite = 4
)

var (
	TemplatesDir = "templates"
	StaticDir    = "static"
)

var (
	monitorContext   context.Context
	monitorWaitGroup sync.WaitGroup
)

func main() {
	var addr, dbFilename, appID, privateKeyFilename, webhookSecret, buildssrhtEndpoint, metasrhtEndpoint, srhtClientID, srhtClientSecret string
	flag.StringVar(&addr, "listen", ":3333", "listening address")
	flag.StringVar(&dbFilename, "db", "hottub.db", "database path")
	flag.StringVar(&appID, "gh-app-id", "", "GitHub app ID")
	flag.StringVar(&privateKeyFilename, "gh-private-key", "", "GitHub app private key path")
	flag.StringVar(&webhookSecret, "gh-webhook-secret", "", "GitHub webhook secret")
	flag.StringVar(&buildssrhtEndpoint, "buildssrht-endpoint", "https://builds.sr.ht", "builds.sr.ht endpoint")
	flag.StringVar(&metasrhtEndpoint, "metasrht-endpoint", "https://meta.sr.ht", "meta.sr.ht endpoint")
	flag.StringVar(&srhtClientID, "metasrht-client-id", "", "meta.sr.ht OAuth2 client ID (optional)")
	flag.StringVar(&srhtClientSecret, "metasrht-client-secret", "", "meta.sr.ht OAuth2 client secret (optional)")
	flag.Parse()

	if appID == "" {
		appID = os.Getenv("GITHUB_APP_IDENTIFIER")
	}
	if privateKeyFilename == "" {
		privateKeyFilename = os.Getenv("GITHUB_PRIVATE_KEY")
	}
	if webhookSecret == "" {
		webhookSecret = os.Getenv("GITHUB_WEBHOOK_SECRET")
	}
	if srhtClientID == "" {
		srhtClientID = os.Getenv("SRHT_CLIENT_ID")
	}
	if srhtClientSecret == "" {
		srhtClientSecret = os.Getenv("SRHT_CLIENT_SECRET")
	}

	if appID == "" || privateKeyFilename == "" {
		log.Fatal("missing -gh-app-id or -gh-private-key")
	}

	atr := createAppsTransport(appID, privateKeyFilename)
	db := createDB(dbFilename)

	agh := github.NewClient(&http.Client{Transport: atr})
	app, _, err := agh.Apps.Get(context.Background(), "")
	if err != nil {
		log.Fatalf("failed to fetch app: %v", err)
	}

	srhtOAuth2Client, err := getSrhtOAuth2Client(metasrhtEndpoint, srhtClientID, srhtClientSecret)
	if err != nil {
		log.Fatalf("failed to create sr.ht OAuth2 client: %v", err)
	}

	tpl := template.Must(template.ParseGlob(TemplatesDir + "/*.html"))

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(StaticDir))))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			App *github.App
		}{
			App: app,
		}
		if err := tpl.ExecuteTemplate(w, "index.html", &data); err != nil {
			panic(err)
		}
	})

	r.HandleFunc("/authorize-srht", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		state, _ := url.ParseQuery(q.Get("state"))
		id, err := strconv.ParseInt(state.Get("installation_id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid state query parameter", http.StatusBadRequest)
			return
		}

		if errCode := q.Get("error"); errCode != "" {
			http.Error(w, fmt.Sprintf("sr.ht error: %v", errCode), http.StatusInternalServerError)
			return
		}

		code := q.Get("code")
		if code == "" {
			http.Error(w, "invalid code query parameter", http.StatusBadRequest)
			return
		}

		installation, err := db.GetInstallation(id)
		if err != nil {
			log.Printf("failed to get installation: %v", err)
			http.Error(w, "failed to get installation", http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		tokenResp, err := srhtOAuth2Client.Exchange(ctx, code)
		if err == nil && tokenResp.TokenType != oauth2.TokenTypeBearer {
			err = fmt.Errorf("unsupported OAuth2 token type %q", tokenResp.TokenType)
		}
		if err != nil {
			log.Printf("failed to exchange sr.ht code for an OAuth2 token: %v", err)
			http.Error(w, "failed to perform OAuth2 exchange", http.StatusInternalServerError)
			return
		}

		if err := saveSrhtToken(ctx, db, buildssrhtEndpoint, srhtOAuth2Client, installation, tokenResp); err != nil {
			log.Print(err)
			http.Error(w, "invalid sr.ht token", http.StatusInternalServerError)
			return
		}

		redirect := fmt.Sprintf("/post-install?installation_id=%d", id)
		http.Redirect(w, r, redirect, http.StatusTemporaryRedirect)
	})

	r.HandleFunc("/post-install", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.URL.Query().Get("installation_id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid installation_id", http.StatusBadRequest)
			return
		}

		installation, err := db.GetInstallation(id)
		if err != nil && err != ErrNotFound {
			log.Printf("failed to get installation: %v", err)
			http.Error(w, "failed to get installation", http.StatusInternalServerError)
			return
		}

		if token := r.FormValue("srht_token"); installation != nil && token != "" && installation.SrhtToken == "" {
			// TODO: a sr.ht user could potentially "steal" a GitHub
			// installation belonging to someone else, by guessing the
			// installation ID before the user has the chance to submit the
			// sr.ht token

			// TODO: discover sr.ht token scope somehow
			tokenResp := &oauth2.TokenResp{
				AccessToken: token,
				TokenType:   oauth2.TokenTypeBearer,
			}
			if err := saveSrhtToken(r.Context(), db, buildssrhtEndpoint, srhtOAuth2Client, installation, tokenResp); err != nil {
				log.Print(err)
				http.Error(w, "invalid sr.ht token", http.StatusBadRequest)
				return
			}
		}

		// If we have a sr.ht client setup, redirect to the sr.ht authorization
		// page
		if installation != nil && installation.SrhtToken == "" && srhtClientID != "" {
			state := make(url.Values)
			state.Set("installation_id", strconv.FormatInt(id, 10))

			redirectURL := srhtOAuth2Client.AuthorizationCodeURL(&oauth2.AuthorizationOptions{
				State: state.Encode(),
				Scope: strings.Split(srhtGrants, " "),
			})
			http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
			return
		}

		var installSettingsURL string
		if installation != nil {
			if installation.Org != "" {
				installSettingsURL = fmt.Sprintf("https://github.com/organizations/%v/settings/installations/%v", installation.Org, id)
			} else {
				installSettingsURL = fmt.Sprintf("https://github.com/settings/installations/%v", id)
			}
		}

		data := struct {
			Pending            bool
			Done               bool
			SrhtGrants         string
			InstallSettingsURL string
		}{
			Pending:            installation == nil,
			Done:               installation != nil && installation.SrhtToken != "",
			SrhtGrants:         srhtGrants,
			InstallSettingsURL: installSettingsURL,
		}
		if err := tpl.ExecuteTemplate(w, "post-install.html", &data); err != nil {
			panic(err)
		}
	})

	r.Post("/webhook", func(w http.ResponseWriter, r *http.Request) {
		payload, err := github.ValidatePayload(r, []byte(webhookSecret))
		if err != nil {
			log.Printf("failed to validate webhook payload: %v", err)
			http.Error(w, "failed to validate webhook paload", http.StatusBadRequest)
			return
		}

		event, err := github.ParseWebHook(github.WebHookType(r), payload)
		if err != nil {
			log.Printf("failed to parse webhook payload: %v", err)
			http.Error(w, "failed to parse webhook paload", http.StatusBadRequest)
			return
		}

		switch event := event.(type) {
		case *github.PingEvent:
			log.Printf("received ping (%v)", *event.Zen)
		case *github.InstallationEvent:
			log.Printf("installation %v by %v", event.GetAction(), event.Sender.GetLogin())
			switch event.GetAction() {
			case "created":
				err = db.StoreInstallation(&Installation{
					ID:        *event.Installation.ID,
					CreatedAt: time.Now(),
					Owner:     event.Sender.GetLogin(),
					Org:       event.GetOrg().GetLogin(),
				})
			case "deleted":
				err = db.DeleteInstallation(*event.Installation.ID)
			}
		case *github.InstallationRepositoriesEvent:
			log.Printf("installation repositories %v by %v (%v added, %v removed)", event.GetAction(), event.Sender.GetLogin(), len(event.RepositoriesAdded), len(event.RepositoriesRemoved))
		case *github.CheckSuiteEvent:
			if *event.Action != "requested" && *event.Action != "rerequested" {
				break
			}

			var installation *Installation
			installation, err = db.GetInstallation(*event.Installation.ID)
			if err != nil {
				break
			}

			if err := refreshSrhtToken(r.Context(), db, srhtOAuth2Client, installation); err != nil {
				log.Printf("failed to refresh sr.ht token for installation %v: %v", installation.ID, err)
			}

			ctx := &checkSuiteContext{
				Context:        r.Context(),
				gh:             newInstallationClient(atr, event.Installation),
				srht:           createSrhtClient(buildssrhtEndpoint, srhtOAuth2Client, installation),
				baseRepo:       event.Repo,
				headRepo:       event.Repo,
				headCommit:     event.CheckSuite.HeadCommit,
				headSHA:        event.CheckSuite.GetHeadSHA(),
				ownerSubmitted: event.Sender.GetLogin() == installation.Owner,
			}
			if len(event.CheckSuite.PullRequests) == 1 {
				ctx.pullRequest = event.CheckSuite.PullRequests[0]
			} else if len(event.CheckSuite.PullRequests) == 0 && event.CheckSuite.HeadBranch != nil {
				ctx.headBranch = *event.CheckSuite.HeadBranch
			}
			err = startCheckSuite(ctx)
		case *github.CheckRunEvent:
			// ignore
		case *github.PullRequestEvent:
			// GitHub doesn't automatically create a CheckSuiteEvent for pull
			// requests made from a fork, so we need to manually handle this
			// case:
			// https://github.community/t/no-check-suite-event-for-foreign-pull-reuqests/13915/2
			if *event.Action != "opened" && *event.Action != "reopened" && *event.Action != "synchronize" {
				break
			}
			if event.PullRequest.Head.Repo.GetFullName() == event.PullRequest.Base.Repo.GetFullName() {
				break
			}

			var installation *Installation
			installation, err = db.GetInstallation(*event.Installation.ID)
			if err != nil {
				break
			}

			ctx := &checkSuiteContext{
				Context:        r.Context(),
				gh:             newInstallationClient(atr, event.Installation),
				srht:           createSrhtClient(buildssrhtEndpoint, srhtOAuth2Client, installation),
				baseRepo:       event.Repo,
				headRepo:       event.PullRequest.Head.Repo,
				headSHA:        event.PullRequest.Head.GetSHA(),
				pullRequest:    event.PullRequest,
				ownerSubmitted: event.Sender.GetLogin() == installation.Owner,
			}

			var repoCommit *github.RepositoryCommit
			repoCommit, _, err = ctx.gh.Repositories.GetCommit(ctx, ctx.headRepo.Owner.GetLogin(), ctx.headRepo.GetName(), ctx.headSHA, nil)
			if err != nil {
				break
			}
			ctx.headCommit = repoCommit.Commit

			err = startCheckSuite(ctx)
		default:
			log.Printf("unhandled event type: %T", event)
		}

		if err != nil {
			log.Printf("failed to handle event %T: %v", event, err)
			http.Error(w, "failed to handle event", http.StatusInternalServerError)
		}
	})

	server := &http.Server{Addr: addr, Handler: r}

	var cancelMonitor context.CancelFunc
	monitorContext, cancelMonitor = context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh

		cancelMonitor()

		log.Printf("Shutting down server")
		if err := server.Shutdown(context.Background()); err != nil {
			log.Fatalf("failed to shutdown server: %v", err)
		}

		// By this point, no more incoming HTTP requests are handled
		monitorWaitGroup.Wait()
	}()

	log.Printf("Server listening on %v", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("failed to listen and serve: %v", err)
	}
}

// userError is a configuration error on the user's end.
type userError struct {
	error
}

type checkSuiteContext struct {
	context.Context
	gh                 *github.Client
	srht               *SrhtClient
	baseRepo, headRepo *github.Repository
	headSHA            string
	headCommit         *github.Commit
	ownerSubmitted     bool

	pullRequest *github.PullRequest // may be nil
	headBranch  string              // may be empty
}

func startCheckSuite(ctx *checkSuiteContext) (err error) {
	defer func() {
		if err == nil {
			return
		}

		msg := "internal error"
		if userErr, ok := err.(userError); ok {
			msg = userErr.Error()
			err = nil
		}

		statusContext := "builds.sr.ht"
		repoStatus := &github.RepoStatus{Context: &statusContext}
		statusErr := updateRepoStatus(ctx, repoStatus, "failure", msg)
		if statusErr != nil {
			log.Printf("failed to create commit status: %v", statusErr)
		}
	}()

	filenames, err := listManifestCandidates(ctx, ctx.gh, ctx.headRepo.Owner.GetLogin(), ctx.headRepo.GetName(), ctx.headSHA)
	if err != nil {
		return err
	}

	// Select a few manifests at random if there are too many
	if len(filenames) > maxJobsPerCheckSuite {
		rand.Shuffle(len(filenames), func(i, j int) {
			filenames[i], filenames[j] = filenames[j], filenames[i]
		})
		filenames = filenames[:maxJobsPerCheckSuite]
	}

	for _, filename := range filenames {
		if err := startJob(ctx, filename); err != nil {
			return err
		}
	}

	return nil
}

func startJob(ctx *checkSuiteContext, filename string) error {
	basename := path.Base(filename)
	name := strings.TrimSuffix(basename, path.Ext(basename))
	if filename == ".build.yml" {
		name = ""
	}

	manifest, err := fetchManifest(ctx, ctx.gh, ctx.headRepo.Owner.GetLogin(), ctx.headRepo.GetName(), ctx.headSHA, filename)
	if err != nil {
		return err
	} else if manifest == nil {
		return nil
	}

	sourcesIface, ok := manifest["sources"]
	if ok {
		cloneURL, err := url.Parse(ctx.headRepo.GetCloneURL())
		if err != nil {
			return userError{fmt.Errorf("failed to parse GitHub clone URL: %v", err)}
		}

		manifestCloneURL := *cloneURL
		manifestCloneURL.Fragment = ctx.headSHA

		sources, ok := sourcesIface.([]interface{})
		if !ok {
			return userError{fmt.Errorf("invalid manifest: `sources` is not a list")}
		}

		for i, srcIface := range sources {
			src, ok := srcIface.(string)
			if !ok {
				return userError{fmt.Errorf("invalid manifest: `sources` contains a %T, want a string", srcIface)}
			}

			// A default branch may be specified in the manifest
			if i := strings.LastIndex(src, "#"); i >= 0 {
				src = src[:i]
			}

			// TODO: use Repo.Parent to figure out whether we should replace
			// the source
			if strings.HasSuffix(src, "/"+ctx.headRepo.GetName()) || strings.HasSuffix(src, "/"+ctx.headRepo.GetName()+".git") {
				sources[i] = manifestCloneURL.String()
			}
		}
	}

	envIface, ok := manifest["environment"]
	if !ok {
		envIface = make(map[string]interface{})
		manifest["environment"] = envIface
	}
	env, ok := envIface.(map[string]interface{})
	if !ok {
		return userError{fmt.Errorf("invalid manifest: `environment` is not a map with string keys")}
	}
	env["BUILD_SUBMITTER"] = "hottub"

	manifestBuf, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %v", err)
	}

	tags := []string{ctx.baseRepo.GetName()}
	if ctx.pullRequest != nil {
		tags = append(tags, "pulls", fmt.Sprintf("%v", ctx.pullRequest.GetNumber()))
	} else if ctx.headBranch != "" {
		tags = append(tags, "commits", ctx.headBranch)
	}
	if name != "" {
		tags = append(tags, name)
	}

	visibility := buildssrht.VisibilityPublic
	if *ctx.headRepo.Private {
		visibility = buildssrht.VisibilityPrivate
	}

	commit := ctx.headCommit
	title := strings.SplitN(commit.GetMessage(), "\n", 2)[0]
	shortHash := ctx.headSHA[0:10]
	commitURL := ctx.headRepo.GetHTMLURL() + "/commit/" + ctx.headSHA
	note := fmt.Sprintf(`%v

[%v] — %v

[%v]: %v`, title, shortHash, commit.Author.GetName(), shortHash, commitURL)

	// Use automatic secrets (nil) if the account owner submitted the job
	var includeSecrets *bool = nil
	if !ctx.ownerSubmitted {
		falseValue := false
		includeSecrets = &falseValue
	}

	job, err := buildssrht.SubmitJob(ctx.srht.GQL, ctx, string(manifestBuf), tags, &note, includeSecrets, visibility)
	if err != nil {
		var httpErr *gqlclient.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusForbidden {
			return userError{fmt.Errorf("failed to submit sr.ht job: %v", err)}
		} else {
			return fmt.Errorf("failed to submit sr.ht job: %v", err)
		}
	}

	detailsURL := fmt.Sprintf("%v/%v/job/%v", ctx.srht.Endpoint, job.Owner.CanonicalName, job.Id)
	statusContext := "builds.sr.ht"
	if name != "" {
		statusContext += "/" + name
	}
	repoStatus := &github.RepoStatus{TargetURL: &detailsURL, Context: &statusContext}
	err = updateRepoStatus(ctx, repoStatus, "pending", "build started…")
	if err != nil {
		return fmt.Errorf("failed to create commit status: %v", err)
	}

	monitorWaitGroup.Add(1)
	go func() {
		defer monitorWaitGroup.Done()

		childCtx := *ctx
		childCtx.Context = monitorContext

		if err := monitorJob(&childCtx, repoStatus, job.Id); err != nil {
			log.Printf("failed to monitor sr.ht job #%d: %v", job.Id, err)

			failBareCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			failCtx := childCtx
			failCtx.Context = failBareCtx

			updateRepoStatus(&failCtx, repoStatus, "failure", "internal error")
		}
	}()

	return nil
}

func monitorJob(ctx *checkSuiteContext, repoStatus *github.RepoStatus, jobID int32) error {
	prevStatus := buildssrht.JobStatusPending
	for {
		time.Sleep(monitorJobInterval)

		var (
			job *buildssrht.Job
			err error
		)
		for i := 0; job == nil && i < monitorMaxRetries; i++ {
			job, err = buildssrht.FetchJob(ctx.srht.GQL, ctx, jobID)
			if err != nil {
				log.Printf("failed to fetch sr.ht job #%v (try %v/%v): %v", jobID, i+1, monitorMaxRetries, err)
				job = nil
				time.Sleep(monitorJobInterval)
			}
		}
		if err != nil {
			return fmt.Errorf("failed to fetch sr.ht job: %v", err)
		}

		if job.Status == prevStatus {
			continue
		}

		state, description := jobStatusToGitHub(job.Status)
		updateRepoStatus(ctx, repoStatus, state, description)

		switch job.Status {
		case buildssrht.JobStatusPending, buildssrht.JobStatusQueued, buildssrht.JobStatusRunning:
			// Continue
		default:
			return nil
		}
	}
}

func jobStatusToGitHub(jobStatus buildssrht.JobStatus) (state, description string) {
	switch jobStatus {
	case buildssrht.JobStatusPending:
		return "pending", "job pending…"
	case buildssrht.JobStatusQueued:
		return "pending", "job queued…"
	case buildssrht.JobStatusRunning:
		return "pending", "job running…"
	case buildssrht.JobStatusSuccess:
		return "success", "job completed"
	case buildssrht.JobStatusFailed:
		return "error", "job failed"
	case buildssrht.JobStatusTimeout:
		return "failure", "job timed out"
	case buildssrht.JobStatusCancelled:
		return "failure", "job cancelled"
	default:
		panic(fmt.Sprintf("unknown sr.ht job status: %v", jobStatus))
	}
}

func updateRepoStatus(ctx *checkSuiteContext, repoStatus *github.RepoStatus, state, description string) error {
	// GitHub rejects updates when description exceeds 140 characters
	if len(description) > 140 {
		description = description[:140]
	}
	repoStatus = &github.RepoStatus{
		TargetURL:   repoStatus.TargetURL,
		Context:     repoStatus.Context,
		State:       &state,
		Description: &description,
	}
	_, _, err := ctx.gh.Repositories.CreateStatus(ctx, ctx.baseRepo.Owner.GetLogin(), ctx.baseRepo.GetName(), ctx.headSHA, repoStatus)
	return err
}

func listManifestCandidates(ctx context.Context, gh *github.Client, repoOwner, repoName, ref string) ([]string, error) {
	_, entries, resp, err := gh.Repositories.GetContents(ctx, repoOwner, repoName, ".builds", &github.RepositoryContentGetOptions{
		Ref: ref,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return []string{".build.yml"}, nil
		}
		return nil, fmt.Errorf("failed to list files in .builds: %v", err)
	}

	var candidates []string
	for _, entry := range entries {
		if *entry.Type != "file" || !strings.HasSuffix(*entry.Name, ".yml") {
			continue
		}
		candidates = append(candidates, *entry.Path)
	}

	return candidates, nil
}

func fetchManifest(ctx context.Context, gh *github.Client, repoOwner, repoName, ref, filename string) (map[string]interface{}, error) {
	f, _, resp, err := gh.Repositories.GetContents(ctx, repoOwner, repoName, filename, &github.RepositoryContentGetOptions{
		Ref: ref,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to download %q: %v", filename, err)
	} else if f == nil {
		return nil, fmt.Errorf("%v isn't a file", filename)
	}

	body, err := f.GetContent()
	if err != nil {
		return nil, fmt.Errorf("failed to decode contents of %v: %v", filename, err)
	}

	var manifest map[string]interface{}
	if err := yaml.Unmarshal([]byte(body), &manifest); err != nil {
		return nil, userError{fmt.Errorf("failed to parse manifest at %v: %v", filename, err)}
	}

	return manifest, nil
}
