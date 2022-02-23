package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/google/go-github/v42/github"
	"gopkg.in/yaml.v3"

	"git.sr.ht/~emersion/hottub/buildssrht"
)

const monitorJobInterval = 5 * time.Second

func main() {
	var addr, dbFilename, appID, privateKeyFilename, webhookSecret, srhtEndpoint string
	flag.StringVar(&addr, "listen", ":3333", "listening address")
	flag.StringVar(&dbFilename, "db", "hottub.db", "database path")
	flag.StringVar(&appID, "gh-app-id", "", "GitHub app ID")
	flag.StringVar(&privateKeyFilename, "gh-private-key", "", "GitHub app private key path")
	flag.StringVar(&webhookSecret, "gh-webhook-secret", "", "GitHub webhook secret")
	flag.StringVar(&srhtEndpoint, "buildssrht-endpoint", "https://builds.sr.ht", "builds.sr.ht endpoint")
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

	tpl := template.Must(template.ParseGlob("templates/*.html"))

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

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

			installation.SrhtToken = token
			srht := createSrhtClient(srhtEndpoint, installation)
			user, err := buildssrht.FetchUser(srht.GQL, r.Context())
			if err != nil {
				log.Printf("failed to fetch sr.ht user: %v", err)
				http.Error(w, "invalid sr.ht token", http.StatusBadRequest)
				return
			}

			if err := db.StoreInstallation(installation); err != nil {
				log.Printf("failed to store installation: %v", err)
				http.Error(w, "failed to store installation", http.StatusInternalServerError)
				return
			}

			log.Printf("user %v has completed installation %v", user.CanonicalName, installation.ID)
		}

		data := struct {
			Pending        bool
			Done           bool
			SrhtGrants     string
			InstallationID int64
		}{
			Pending:        installation == nil,
			Done:           installation != nil && installation.SrhtToken != "",
			SrhtGrants:     "builds.sr.ht/PROFILE:RO builds.sr.ht/JOBS:RW",
			InstallationID: id,
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
				})
			case "deleted":
				err = db.DeleteInstallation(*event.Installation.ID)
			}
		case *github.CheckSuiteEvent:
			if *event.Action != "requested" && *event.Action != "rerequested" {
				break
			}

			var installation *Installation
			installation, err = db.GetInstallation(*event.Installation.ID)
			if err != nil {
				break
			}

			ctx := &checkSuiteContext{
				Context:    r.Context(),
				gh:         newInstallationClient(atr, event.Installation),
				srht:       createSrhtClient(srhtEndpoint, installation),
				baseRepo:   event.Repo,
				headRepo:   event.Repo,
				headCommit: event.CheckSuite.HeadCommit,
				headSHA:    event.CheckSuite.GetHeadSHA(),
			}
			if len(event.CheckSuite.PullRequests) == 1 {
				ctx.pullRequest = event.CheckSuite.PullRequests[0]
			} else if len(event.CheckSuite.PullRequests) == 0 && event.CheckSuite.HeadBranch != nil {
				ctx.headBranch = *event.CheckSuite.HeadBranch
			}
			err = startCheckSuite(ctx)
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
				Context:     r.Context(),
				gh:          newInstallationClient(atr, event.Installation),
				srht:        createSrhtClient(srhtEndpoint, installation),
				baseRepo:    event.Repo,
				headRepo:    event.PullRequest.Head.Repo,
				headSHA:     event.PullRequest.Head.GetSHA(),
				pullRequest: event.PullRequest,
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

	log.Printf("Server listening on %v", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}

type checkSuiteContext struct {
	context.Context
	gh                 *github.Client
	srht               *SrhtClient
	baseRepo, headRepo *github.Repository
	headSHA            string
	headCommit         *github.Commit

	pullRequest *github.PullRequest // may be nil
	headBranch  string              // may be empty
}

func startCheckSuite(ctx *checkSuiteContext) error {
	filenames, err := listManifestCandidates(ctx, ctx.gh, ctx.headRepo.Owner.GetLogin(), ctx.headRepo.GetName(), ctx.headSHA)
	if err != nil {
		return err
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
			return fmt.Errorf("failed to parse GitHub clone URL: %v", err)
		}

		manifestCloneURL := *cloneURL
		manifestCloneURL.Fragment = ctx.headSHA

		sources, ok := sourcesIface.([]interface{})
		if !ok {
			return fmt.Errorf("invalid manifest: `sources` is not a list")
		}

		for i, srcIface := range sources {
			src, ok := srcIface.(string)
			if !ok {
				return fmt.Errorf("invalid manifest: `sources` contains a %T, want a string", srcIface)
			}

			// TODO: use Repo.Parent to figure out whether we should replace
			// the source
			if strings.HasSuffix(src, "/"+ctx.headRepo.GetName()) || strings.HasSuffix(src, "/"+ctx.headRepo.GetName()+".git") {
				sources[i] = manifestCloneURL.String()
			}
		}
	}

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

	commit := ctx.headCommit
	title := strings.SplitN(commit.GetMessage(), "\n", 2)[0]
	shortHash := ctx.headSHA[0:10]
	commitURL := ctx.headRepo.GetHTMLURL() + "/commit/" + ctx.headSHA
	note := fmt.Sprintf(`%v

[%v] — %v

[%v]: %v`, title, shortHash, commit.Author.GetName(), shortHash, commitURL)

	job, err := buildssrht.SubmitJob(ctx.srht.GQL, ctx, string(manifestBuf), tags, &note)
	if err != nil {
		return fmt.Errorf("failed to submit sr.ht job: %v", err)
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

	go func() {
		childCtx := *ctx
		childCtx.Context = context.TODO()

		if err := monitorJob(&childCtx, repoStatus, job); err != nil {
			log.Printf("failed to monitor sr.ht job #%d: %v", job.Id, err)
			updateRepoStatus(&childCtx, repoStatus, "failure", "internal error")
		}
	}()

	return nil
}

func monitorJob(ctx *checkSuiteContext, repoStatus *github.RepoStatus, job *buildssrht.Job) error {
	prevStatus := buildssrht.JobStatusPending
	for {
		time.Sleep(monitorJobInterval)

		job, err := buildssrht.FetchJob(ctx.srht.GQL, ctx, job.Id)
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
		return nil, fmt.Errorf("failed to parse manifest at %v: %v", filename, err)
	}

	return manifest, nil
}
