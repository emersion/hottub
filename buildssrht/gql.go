// Code generated by gqlclientgen - DO NOT EDIT

package buildssrht

import (
	"context"
	gqlclient "git.sr.ht/~emersion/gqlclient"
	"time"
)

type AccessKind string

const (
	AccessKindRo AccessKind = "RO"
	AccessKindRw AccessKind = "RW"
)

type AccessScope string

const (
	AccessScopeProfile AccessScope = "PROFILE"
	AccessScopeJobs    AccessScope = "JOBS"
	AccessScopeLogs    AccessScope = "LOGS"
	AccessScopeSecrets AccessScope = "SECRETS"
)

type Artifact struct {
	Id      int32     `json:"id"`
	Created time.Time `json:"created"`
	// Original path in the guest
	Path string `json:"path"`
	// Size in bytes
	Size int32 `json:"size"`
	// URL at which the artifact may be downloaded, or null if pruned
	Url *string `json:"url,omitempty"`
}

type Binary string

type Cursor string

type EmailTrigger struct {
	Condition TriggerCondition `json:"condition"`
	To        string           `json:"to"`
	Cc        *string          `json:"cc,omitempty"`
	InReplyTo *string          `json:"inReplyTo,omitempty"`
}

type EmailTriggerInput struct {
	To        string  `json:"to"`
	Cc        *string `json:"cc,omitempty"`
	InReplyTo *string `json:"inReplyTo,omitempty"`
}

type Entity struct {
	Id      int32     `json:"id"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	// The canonical name of this entity. For users, this is their username
	// prefixed with '~'. Additional entity types will be supported in the future.
	CanonicalName string `json:"canonicalName"`
}

type File string

type Job struct {
	Id       int32     `json:"id"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Status   JobStatus `json:"status"`
	Manifest string    `json:"manifest"`
	Note     *string   `json:"note,omitempty"`
	Tags     []*string `json:"tags"`
	// Name of the build image
	Image string `json:"image"`
	// Name of the build runner which picked up this job, or null if the job is
	// pending or queued.
	Runner    *string     `json:"runner,omitempty"`
	Owner     *Entity     `json:"owner"`
	Group     *JobGroup   `json:"group,omitempty"`
	Tasks     []*Task     `json:"tasks"`
	Artifacts []*Artifact `json:"artifacts"`
	// The job's top-level log file, not associated with any tasks
	Log *Log `json:"log,omitempty"`
	// List of secrets available to this job, or null if they were disabled
	Secrets []*Secret `json:"secrets,omitempty"`
}

// A cursor for enumerating a list of jobs
//
// If there are additional results available, the cursor object may be passed
// back into the same endpoint to retrieve another page. If the cursor is null,
// there are no remaining results to return.
type JobCursor struct {
	Results []Job   `json:"results"`
	Cursor  *Cursor `json:"cursor,omitempty"`
}

type JobGroup struct {
	Id       int32      `json:"id"`
	Created  time.Time  `json:"created"`
	Note     *string    `json:"note,omitempty"`
	Owner    *Entity    `json:"owner"`
	Jobs     []*Job     `json:"jobs"`
	Triggers []*Trigger `json:"triggers"`
}

type JobStatus string

const (
	JobStatusPending   JobStatus = "PENDING"
	JobStatusQueued    JobStatus = "QUEUED"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusSuccess   JobStatus = "SUCCESS"
	JobStatusFailed    JobStatus = "FAILED"
	JobStatusTimeout   JobStatus = "TIMEOUT"
	JobStatusCancelled JobStatus = "CANCELLED"
)

type Log struct {
	// The most recently written 128 KiB of the build log.
	Last128KiB string `json:"last128KiB"`
	// The URL at which the full build log can be downloaded with a GET request
	// (text/plain).
	FullURL string `json:"fullURL"`
}

type PGPKey struct {
	Id         int32     `json:"id"`
	Created    time.Time `json:"created"`
	Uuid       string    `json:"uuid"`
	Name       *string   `json:"name,omitempty"`
	PrivateKey Binary    `json:"privateKey"`
}

type SSHKey struct {
	Id         int32     `json:"id"`
	Created    time.Time `json:"created"`
	Uuid       string    `json:"uuid"`
	Name       *string   `json:"name,omitempty"`
	PrivateKey Binary    `json:"privateKey"`
}

type Secret struct {
	Id      int32     `json:"id"`
	Created time.Time `json:"created"`
	Uuid    string    `json:"uuid"`
	Name    *string   `json:"name,omitempty"`
}

// A cursor for enumerating a list of secrets
//
// If there are additional results available, the cursor object may be passed
// back into the same endpoint to retrieve another page. If the cursor is null,
// there are no remaining results to return.
type SecretCursor struct {
	Results []Secret `json:"results"`
	Cursor  *Cursor  `json:"cursor,omitempty"`
}

type SecretFile struct {
	Id      int32     `json:"id"`
	Created time.Time `json:"created"`
	Uuid    string    `json:"uuid"`
	Name    *string   `json:"name,omitempty"`
	Path    string    `json:"path"`
	Mode    int32     `json:"mode"`
	Data    Binary    `json:"data"`
}

type Settings struct {
	SshUser      string `json:"sshUser"`
	BuildTimeout string `json:"buildTimeout"`
}

type Task struct {
	Id      int32      `json:"id"`
	Created time.Time  `json:"created"`
	Updated time.Time  `json:"updated"`
	Name    string     `json:"name"`
	Status  TaskStatus `json:"status"`
	Log     *Log       `json:"log,omitempty"`
	Job     *Job       `json:"job"`
}

type TaskStatus string

const (
	TaskStatusPending TaskStatus = "PENDING"
	TaskStatusRunning TaskStatus = "RUNNING"
	TaskStatusSuccess TaskStatus = "SUCCESS"
	TaskStatusFailed  TaskStatus = "FAILED"
	TaskStatusSkipped TaskStatus = "SKIPPED"
)

// Triggers run upon the completion of all of the jobs in a job group. Note that
// these triggers are distinct from the ones defined by an individual job's
// build manifest, but are similar in functionality.
type Trigger struct {
	Condition TriggerCondition `json:"condition"`
}

type TriggerCondition string

const (
	TriggerConditionSuccess TriggerCondition = "SUCCESS"
	TriggerConditionFailure TriggerCondition = "FAILURE"
	TriggerConditionAlways  TriggerCondition = "ALWAYS"
)

type TriggerInput struct {
	Type      TriggerType          `json:"type"`
	Condition TriggerCondition     `json:"condition"`
	Email     *EmailTriggerInput   `json:"email,omitempty"`
	Webhook   *WebhookTriggerInput `json:"webhook,omitempty"`
}

type TriggerType string

const (
	TriggerTypeEmail   TriggerType = "EMAIL"
	TriggerTypeWebhook TriggerType = "WEBHOOK"
)

type User struct {
	Id            int32     `json:"id"`
	Created       time.Time `json:"created"`
	Updated       time.Time `json:"updated"`
	CanonicalName string    `json:"canonicalName"`
	Username      string    `json:"username"`
	Email         string    `json:"email"`
	Url           *string   `json:"url,omitempty"`
	Location      *string   `json:"location,omitempty"`
	Bio           *string   `json:"bio,omitempty"`
	// Jobs submitted by this user.
	Jobs *JobCursor `json:"jobs"`
}

type Version struct {
	Major int32 `json:"major"`
	Minor int32 `json:"minor"`
	Patch int32 `json:"patch"`
	// If this API version is scheduled for deprecation, this is the date on which
	// it will stop working; or null if this API version is not scheduled for
	// deprecation.
	DeprecationDate time.Time `json:"deprecationDate,omitempty"`
	Settings        *Settings `json:"settings"`
}

type WebhookTrigger struct {
	Condition TriggerCondition `json:"condition"`
	Url       string           `json:"url"`
}

type WebhookTriggerInput struct {
	Url string `json:"url"`
}

func SubmitJob(client *gqlclient.Client, ctx context.Context, manifest string, tags []string, note *string) (submit *Job, err error) {
	op := gqlclient.NewOperation("mutation submitJob ($manifest: String!, $tags: [String!], $note: String) {\n\tsubmit(manifest: $manifest, secrets: false, tags: $tags, note: $note) {\n\t\tid\n\t\towner {\n\t\t\tcanonicalName\n\t\t}\n\t}\n}\n")
	op.Var("manifest", manifest)
	op.Var("tags", tags)
	op.Var("note", note)
	var respData struct {
		Submit *Job
	}
	err = client.Execute(ctx, op, &respData)
	return respData.Submit, err
}

func FetchJob(client *gqlclient.Client, ctx context.Context, id int32) (job *Job, err error) {
	op := gqlclient.NewOperation("query fetchJob ($id: Int!) {\n\tjob(id: $id) {\n\t\tstatus\n\t}\n}\n")
	op.Var("id", id)
	var respData struct {
		Job *Job
	}
	err = client.Execute(ctx, op, &respData)
	return respData.Job, err
}

func FetchUser(client *gqlclient.Client, ctx context.Context) (me *User, err error) {
	op := gqlclient.NewOperation("query fetchUser {\n\tme {\n\t\tcanonicalName\n\t}\n}\n")
	var respData struct {
		Me *User
	}
	err = client.Execute(ctx, op, &respData)
	return respData.Me, err
}
