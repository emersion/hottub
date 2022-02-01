package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
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
	atr := createAppsTransport()
	webhookSecret := []byte(os.Getenv("GITHUB_WEBHOOK_SECRET"))
	db := createDB("hottub.db")

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

		if token := r.FormValue("srht_token"); installation != nil && token != "" {
			// TODO: a sr.ht user could potentially "steal" a GitHub
			// installation belonging to someone else, by guessing the
			// installation ID before the user has the chance to submit the
			// sr.ht token

			installation.SrhtToken = token
			srht := createSrhtClient(installation)
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
			Pending    bool
			Done       bool
			SrhtGrants string
		}{
			Pending:    installation == nil,
			Done:       installation != nil && installation.SrhtToken != "",
			SrhtGrants: "builds.sr.ht/PROFILE:RO builds.sr.ht/JOBS:RW",
		}
		if err := tpl.ExecuteTemplate(w, "post-install.html", &data); err != nil {
			panic(err)
		}
	})

	r.Post("/webhook", func(w http.ResponseWriter, r *http.Request) {
		payload, err := github.ValidatePayload(r, webhookSecret)
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
		case *github.InstallationEvent:
			log.Printf("installation %v by %v", event.GetAction(), event.Sender.GetLogin())
			switch event.GetAction() {
			case "created":
				err = db.StoreInstallation(&Installation{
					ID: *event.Installation.ID,
				})
			case "deleted":
				err = db.DeleteInstallation(*event.Installation.ID)
			}
		case *github.CheckSuiteEvent:
			gh := newInstallationClient(atr, event.Installation)

			var installation *Installation
			installation, err = db.GetInstallation(*event.Installation.ID)
			if err != nil {
				break
			}
			srht := createSrhtClient(installation)

			switch event.GetAction() {
			case "requested", "rerequested":
				err = startCheckSuite(r.Context(), gh, srht, event)
			}
		default:
			log.Printf("unhandled event type: %T", event)
		}

		if err != nil {
			log.Printf("failed to handle event %T: %v", event, err)
			http.Error(w, "failed to handle event", http.StatusInternalServerError)
		}
	})

	log.Fatal(http.ListenAndServe(":3333", r))
}

func startCheckSuite(ctx context.Context, gh *github.Client, srht *SrhtClient, event *github.CheckSuiteEvent) error {
	repoOwner, repoName := *event.Repo.Owner.Login, *event.Repo.Name

	manifest, err := fetchManifest(ctx, gh, repoOwner, repoName, *event.CheckSuite.HeadSHA)
	if err != nil {
		return err
	} else if manifest == nil {
		return nil
	}

	sourcesIface, ok := manifest["sources"]
	if ok {
		cloneURL, err := url.Parse(*event.Repo.CloneURL)
		if err != nil {
			return fmt.Errorf("failed to parse GitHub clone URL: %v", err)
		}

		manifestCloneURL := *cloneURL
		manifestCloneURL.Fragment = *event.CheckSuite.HeadSHA

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
			if strings.HasSuffix(src, "/"+repoName) || strings.HasSuffix(src, "/"+repoName+".git") {
				sources[i] = manifestCloneURL.String()
			}
		}
	}

	manifestBuf, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %v", err)
	}

	tags := []string{repoName}
	if len(event.CheckSuite.PullRequests) > 1 {
		tags = append(tags, "pulls")
	} else if len(event.CheckSuite.PullRequests) == 1 {
		tags = append(tags, "pulls", fmt.Sprintf("%v", *event.CheckSuite.PullRequests[0].Number))
	} else if event.CheckSuite.HeadBranch != nil {
		tags = append(tags, "commits", *event.CheckSuite.HeadBranch)
	}

	commit := event.CheckSuite.HeadCommit
	title := strings.SplitN(*commit.Message, "\n", 2)[0]
	shortHash := (*event.CheckSuite.HeadSHA)[0:10]
	commitURL := strings.ReplaceAll(*event.Repo.CommitsURL, "{/sha}", *event.CheckSuite.HeadSHA)
	note := fmt.Sprintf(`%v

[%v] — %v

[%v]: %v`, title, shortHash, *commit.Author.Name, shortHash, commitURL)

	job, err := buildssrht.SubmitJob(srht.GQL, ctx, string(manifestBuf), tags, &note)
	if err != nil {
		return fmt.Errorf("failed to submit sr.ht job: %v", err)
	}

	detailsURL := fmt.Sprintf("%v/%v/job/%v", srht.Endpoint, job.Owner.CanonicalName, job.Id)
	externalID := fmt.Sprintf("%v", job.Id)
	checkRun, _, err := gh.Checks.CreateCheckRun(ctx, repoOwner, repoName, github.CreateCheckRunOptions{
		Name:       "builds.sr.ht",
		HeadSHA:    *event.CheckSuite.HeadSHA,
		DetailsURL: &detailsURL,
		ExternalID: &externalID,
	})
	if err != nil {
		return fmt.Errorf("failed to create check run: %v", err)
	}

	go func() {
		ctx := context.TODO()

		if err := monitorJob(ctx, gh, srht, repoOwner, repoName, checkRun, job); err != nil {
			log.Printf("failed to monitor sr.ht job #%d: %v", job.Id, err)
			updateCheckRun(ctx, gh, repoOwner, repoName, checkRun, "completed", "failure")
		}
	}()

	return nil
}

func monitorJob(ctx context.Context, gh *github.Client, srht *SrhtClient, repoOwner, repoName string, checkRun *github.CheckRun, job *buildssrht.Job) error {
	prevStatus := buildssrht.JobStatusPending
	for {
		time.Sleep(monitorJobInterval)

		job, err := buildssrht.FetchJob(srht.GQL, ctx, job.Id)
		if err != nil {
			return fmt.Errorf("failed to fetch sr.ht job: %v", err)
		}

		if job.Status == prevStatus {
			continue
		}

		status, conclusion := jobStatusToGitHub(job.Status)
		updateCheckRun(ctx, gh, repoOwner, repoName, checkRun, status, conclusion)

		switch job.Status {
		case buildssrht.JobStatusPending, buildssrht.JobStatusQueued, buildssrht.JobStatusRunning:
			// Continue
		default:
			return nil
		}
	}
}

func jobStatusToGitHub(jobStatus buildssrht.JobStatus) (status, conclusion string) {
	switch jobStatus {
	case buildssrht.JobStatusPending, buildssrht.JobStatusQueued:
		return "queued", ""
	case buildssrht.JobStatusRunning:
		return "in_progress", ""
	case buildssrht.JobStatusSuccess:
		return "completed", "success"
	case buildssrht.JobStatusFailed:
		return "completed", "failure"
	case buildssrht.JobStatusTimeout:
		return "completed", "timed_out"
	case buildssrht.JobStatusCancelled:
		return "completed", "cancelled"
	default:
		panic(fmt.Sprintf("unknown sr.ht job status: %v", jobStatus))
	}
}

func updateCheckRun(ctx context.Context, gh *github.Client, repoOwner, repoName string, checkRun *github.CheckRun, status, conclusion string) error {
	conclusionPtr := &conclusion
	if conclusion == "" {
		conclusionPtr = nil
	}
	_, _, err := gh.Checks.UpdateCheckRun(ctx, repoOwner, repoName, *checkRun.ID, github.UpdateCheckRunOptions{
		Name:       *checkRun.Name,
		Status:     &status,
		Conclusion: conclusionPtr,
	})
	return err
}

func fetchManifest(ctx context.Context, gh *github.Client, repoOwner, repoName, ref string) (map[string]interface{}, error) {
	f, _, resp, err := gh.Repositories.GetContents(ctx, repoOwner, repoName, ".build.yml", &github.RepositoryContentGetOptions{
		Ref: ref,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to download .build.yml: %v", err)
	} else if f == nil {
		return nil, fmt.Errorf(".build.yml isn't a file")
	}

	body, err := f.GetContent()
	if err != nil {
		return nil, fmt.Errorf("failed to decode file contents: %v", err)
	}

	var manifest map[string]interface{}
	if err := yaml.Unmarshal([]byte(body), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %v", err)
	}

	return manifest, nil
}
