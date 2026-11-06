package api

import (
	"fmt"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/notify"
)

// Chat notifications are advisory: they are queued, may be dropped when the queue is
// full and never influence the request that raised them.

func (a *API) linkTo(path string) string { return a.Cfg.PublicURL + path }

// notifyApproval announces a human-in-the-loop request.
func (a *API) notifyApproval(ag *domain.Agent, in *domain.Interrupt) {
	a.Notify.Publish(notify.Event{
		Kind:  notify.KindApproval,
		Title: "Approval needed: " + truncate(in.Question, 120),
		Text:  fmt.Sprintf("%s is waiting for a human decision.", ag.Name),
		URL:   a.linkTo("/approvals"),
		Fields: [][2]string{
			{"Agent", ag.Name},
			{"Node", in.Node},
			{"Run", in.RunID},
		},
	})
}

// notifyRunFailed announces a run that ended in an error.
func (a *API) notifyRunFailed(ag *domain.Agent, run *domain.Run) {
	a.Notify.Publish(notify.Event{
		Kind:  notify.KindRunFailed,
		Title: "Run failed on " + ag.Name,
		Text:  truncate(run.Error, 500),
		URL:   a.linkTo("/runs/" + run.ID),
		Fields: [][2]string{
			{"Agent", ag.Name},
			{"Run", run.ID},
		},
	})
}
