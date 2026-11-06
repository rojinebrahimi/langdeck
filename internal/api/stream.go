package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// stream pushes change notifications to the UI over Server-Sent Events.
// SSE is one-way, works through most proxies, and reconnects by itself.
func (a *API) stream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // the server-wide write timeout would cut the stream

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "retry: 3000\n\n")
	if err := rc.Flush(); err != nil {
		return
	}

	sub, cancel := a.Broker.Subscribe()
	defer cancel()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case e := <-sub:
			b, _ := json.Marshal(e)
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Kind, b); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
