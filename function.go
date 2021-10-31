package function

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/xanzy/go-gitlab"
)

// TODO: make easier to customise.
const (
	cardTitle             = "Biweekly Summary"
	reviewRequiredLabel   = "review required"
	changesRequestedLabel = "changes requested"
)

type MessageCard struct {
	Type             string            `json:"@type"`
	Context          string            `json:"@context"`
	Summary          string            `json:"summary,omitempty"`
	Title            string            `json:"title,omitempty"`
	Text             string            `json:"text,omitempty"`
	ThemeColor       string            `json:"themeColor,omitempty"`
	Sections         []Section         `json:"sections,omitempty"`
	PotentialActions []PotentialAction `json:"potentialAction,omitempty"`
}

type Section struct {
	Title            string `json:"title,omitempty"`
	ActivityImage    string `json:"activityImage,omitempty"`
	ActivityTitle    string `json:"activityTitle,omitempty"`
	ActivitySubtitle string `json:"activitySubtitle,omitempty"`
	Facts            []Fact `json:"facts,omitempty"`
	Text             string `json:"text,omitempty"`
	StartGroup       bool   `json:"startGroup,omitempty"`
}

type Fact struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type PotentialAction struct {
	Type    string              `json:"@type"`
	Name    string              `json:"name"`
	Targets []map[string]string `json:"targets,omitempty"`
}

type RawMergeRequest struct {
	ID                          int               `json:"id"`
	IID                         int               `json:"iid"`
	Title                       string            `json:"title"`
	CreatedAt                   *time.Time        `json:"created_at"`
	UpdatedAt                   *time.Time        `json:"updated_at"`
	Author                      *gitlab.BasicUser `json:"author"`
	Labels                      []interface{}     `json:"labels"`
	Description                 string            `json:"description"`
	WorkInProgress              bool              `json:"work_in_progress"`
	MergeStatus                 string            `json:"merge_status"`
	ClosedAt                    *time.Time        `json:"closed_at"`
	WebURL                      string            `json:"web_url"`
	HasConflicts                bool              `json:"has_conflicts"`
	BlockingDiscussionsResolved bool              `json:"blocking_discussions_resolved"`
}

type MergeRequest struct {
	ID              int
	Title           string
	WebURL          string
	Author          string
	AuthorAvatarURL string

	CreatedAt *time.Time
	UpdatedAt *time.Time

	ReviewRequiredAt   *time.Time
	ChangesRequestedAt *time.Time

	HasConflicts                bool
	BlockingDiscussionsResolved bool
	IsMergeable                 bool
}

func F(w http.ResponseWriter, r *http.Request) {
	teamsWebhookURL := os.Getenv("TEAMS_WEBHOOK_URL")
	if teamsWebhookURL == "" {
		log.Fatalln("`TEAMS_WEBHOOK_URL` is not set in the environment")
	}

	if _, err := url.Parse(teamsWebhookURL); err != nil {
		log.Fatalln(err)
	}

	gitlabAPIToken := os.Getenv("GITLAB_API_TOKEN")
	if gitlabAPIToken == "" {
		log.Fatalln("`GITLAB_API_TOKEN` is not set in the environment")
	}

	gitlabProjectID := os.Getenv("GITLAB_PROJECT_ID")
	if gitlabProjectID == "" {
		log.Fatalln("`GITLAB_PROJECT_ID` is not set in the environment")
	}

	client, err := gitlab.NewClient(gitlabAPIToken)
	if err != nil {
		log.Fatalln(err)
	}

	p, _, err := client.Projects.GetProject(gitlabProjectID, nil)
	if err != nil {
		log.Fatalln(err)
	}
	projectURL := p.WebURL

	opts := &gitlab.ListProjectMergeRequestsOptions{
		Sort:  gitlab.String("asc"),
		State: gitlab.String("opened"),
		WIP:   gitlab.String("no"),
	}

	// HACK: we are not using the GitLab Go client's `ListProjectMergeRequests`
	// because it does not handle labels well, resulting in errors.
	// Though, this may well be an issue on GitLab API side (returning
	// inconsistent results).
	path := fmt.Sprintf("projects/%s/merge_requests", gitlabProjectID)
	req, err := client.NewRequest(http.MethodGet, path, opts, nil)
	if err != nil {
		log.Fatalln(err)
	}

	var mergeRequests []*RawMergeRequest
	_, err = client.Do(req, &mergeRequests)
	if err != nil {
		log.Fatalln(err)
	}

	var summarisedMRs []MergeRequest

	for _, mergeRequest := range mergeRequests {
		if mergeRequest.ClosedAt != nil {
			continue
		}

		if mergeRequest.WorkInProgress {
			continue
		}

		if len(mergeRequest.Labels) <= 0 {
			continue
		}

		labelEvents, _, err := client.ResourceLabelEvents.ListMergeRequestsLabelEvents(gitlabProjectID, mergeRequest.IID, &gitlab.ListLabelEventsOptions{})
		if err != nil {
			log.Fatalln(err)
		}

		summarisedMR := summariseMergeRequest(mergeRequest, labelEvents)
		summarisedMRs = append(summarisedMRs, summarisedMR)
	}

	messageCard := toMessageCard(projectURL, summarisedMRs)

	payload, err := json.Marshal(messageCard)
	if err != nil {
		log.Fatalln(err)
	}

	res, err := http.Post(teamsWebhookURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		log.Fatalln(err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Println("payload", string(payload))
		log.Fatalln("unexpected status code", res.StatusCode)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(messageCard)
	if err != nil {
		log.Fatalln(err)
	}
}

func toMessageCard(projectURL string, mergeRequests []MergeRequest) *MessageCard {
	var reviewRequired []MergeRequest
	var changesRequested []MergeRequest

	for _, mergeRequest := range mergeRequests {
		if mergeRequest.ReviewRequiredAt != nil {
			reviewRequired = append(reviewRequired, mergeRequest)
		}
		if mergeRequest.ChangesRequestedAt != nil {
			changesRequested = append(changesRequested, mergeRequest)
		}
	}

	var sections []Section

	if len(reviewRequired) > 0 {
		sections = append(sections, Section{
			Title: "**Review Required**",
		})

		for i, mergeRequest := range reviewRequired {
			if i >= 6 {
				break
			}
			section := Section{
				ActivityTitle:    fmt.Sprintf("[%s](%s)", mergeRequest.Title, mergeRequest.WebURL),
				ActivitySubtitle: mergeRequest.Author,
				ActivityImage:    mergeRequest.AuthorAvatarURL,
				Facts: []Fact{
					{
						Name:  "Created",
						Value: getCreated(mergeRequest),
					},
					{
						Name:  "Review Required",
						Value: getReviewRequired(mergeRequest),
					},
					{
						Name:  "Is Mergeable?",
						Value: getIsMergeable(mergeRequest),
					},
				},
				StartGroup: i != 0,
			}
			sections = append(sections, section)
		}
	}

	if len(changesRequested) > 0 {
		sections = append(sections, Section{
			Title:      "**Changes Requested**",
			StartGroup: true,
		})

		for i, mergeRequest := range changesRequested {
			if i >= 6 {
				break
			}
			section := Section{
				ActivityTitle:    fmt.Sprintf("[%s](%s)", mergeRequest.Title, mergeRequest.WebURL),
				ActivitySubtitle: mergeRequest.Author,
				ActivityImage:    mergeRequest.AuthorAvatarURL,
				Facts: []Fact{
					{
						Name:  "Created",
						Value: getCreated(mergeRequest),
					},
					{
						Name:  "Changes Requested",
						Value: getChangesRequested(mergeRequest),
					},
					{
						Name:  "Is Mergeable?",
						Value: getIsMergeable(mergeRequest),
					},
				},
				StartGroup: i != 0,
			}
			sections = append(sections, section)
		}
	}

	if len(sections) <= 0 {
		return nil
	}

	summary := fmt.Sprintf("MRs Pending Review: %d (or more) | MRs Pending Changes: %d (or more)", len(reviewRequired), len(changesRequested))

	potentialActions := []PotentialAction{}
	if projectURL != "" {
		potentialActions = append(potentialActions, PotentialAction{
			Type: "OpenUri",
			Name: "View in GitLab",
			Targets: []map[string]string{
				{
					"os":  "default",
					"uri": projectURL,
				},
			},
		})
	}

	return &MessageCard{
		Type:             "MessageCard",
		Context:          "https://schema.org/extensions",
		Summary:          summary,
		Title:            cardTitle,
		Text:             `Click "See more" below to show the full list of MRs. This summary will only include the oldest 6.`,
		ThemeColor:       "13c2c2",
		Sections:         sections,
		PotentialActions: potentialActions,
	}
}

func getCreated(mergeRequest MergeRequest) string {
	created := "N/A"
	if mergeRequest.CreatedAt != nil {
		created = humanize.RelTime(*mergeRequest.CreatedAt, time.Now().UTC(), "ago", "")
		if mergeRequest.UpdatedAt != nil {
			created += fmt.Sprintf(" (updated %s)", humanize.RelTime(*mergeRequest.UpdatedAt, time.Now().UTC(), "ago", ""))
		}
	}
	return created
}

func getReviewRequired(mergeRequest MergeRequest) string {
	reviewRequired := "N/A"
	if mergeRequest.ReviewRequiredAt != nil {
		reviewRequired = humanize.RelTime(*mergeRequest.ReviewRequiredAt, time.Now().UTC(), "ago", "")
	}
	return reviewRequired
}

func getChangesRequested(mergeRequest MergeRequest) string {
	changesRequested := "N/A"
	if mergeRequest.ChangesRequestedAt != nil {
		changesRequested = humanize.RelTime(*mergeRequest.ChangesRequestedAt, time.Now().UTC(), "ago", "")
	}
	return changesRequested
}

func getIsMergeable(mergeRequest MergeRequest) string {
	isMergeable := "❓"
	if mergeRequest.HasConflicts {
		isMergeable = "⚠️ _Merge Conflicts_"
	} else if !mergeRequest.BlockingDiscussionsResolved {
		isMergeable = "⚠️ _Unresolved Discussions_"
	} else if mergeRequest.IsMergeable {
		isMergeable = "✅"
	}
	return isMergeable
}

func summariseMergeRequest(mr *RawMergeRequest, labelEvents []*gitlab.LabelEvent) MergeRequest {
	summarisedMR := MergeRequest{
		ID:                          mr.ID,
		Title:                       mr.Title,
		WebURL:                      mr.WebURL,
		Author:                      mr.Author.Name,
		AuthorAvatarURL:             mr.Author.AvatarURL,
		CreatedAt:                   mr.CreatedAt,
		UpdatedAt:                   mr.UpdatedAt,
		HasConflicts:                mr.HasConflicts,
		BlockingDiscussionsResolved: mr.BlockingDiscussionsResolved,
		IsMergeable:                 strings.EqualFold(mr.MergeStatus, "can_be_merged"),
	}

	for i := len(labelEvents) - 1; i >= 0; i-- {
		labelEvent := labelEvents[i]
		if !strings.EqualFold(labelEvent.Action, "add") {
			continue
		}

		if strings.EqualFold(labelEvent.Label.Name, reviewRequiredLabel) {
			summarisedMR.ReviewRequiredAt = labelEvent.CreatedAt
			break
		}

		if strings.EqualFold(labelEvent.Label.Name, changesRequestedLabel) {
			summarisedMR.ChangesRequestedAt = labelEvent.CreatedAt
			break
		}
	}

	return summarisedMR
}
