package server

import "net/http"

func (c *controllerV1) handleGetMessageReview(w http.ResponseWriter, r *http.Request) {
	msg, err := c.backend.LoadMessageReview(r.Context(), r.PathValue("id"), r.PathValue("sid"), r.PathValue("mid"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, messageToProto(msg))
}
