package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
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
	srht := createSrhtClient()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
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
		case *github.CheckSuiteEvent:
			gh := newInstallationClient(atr, event.Installation)
			if *event.Action == "requested" || *event.Action == "rerequested" {
				if err := startCheckSuite(r.Context(), gh, srht, event); err != nil {
					log.Printf("failed to start check suite: %v", err)
					http.Error(w, "failed to start check suite", http.StatusInternalServerError)
					return
				}
			}
		default:
			log.Printf("unhandled event type: %T", event)
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

	manifestBuf, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %v", err)
	}

	job, err := buildssrht.SubmitJob(srht.GQL, ctx, string(manifestBuf))
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
