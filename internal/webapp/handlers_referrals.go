package webapp

import "net/http"

func (h *Handler) handleReferralsList(w http.ResponseWriter, r *http.Request) {
	limit, offset, page := pagination(r)
	refs, total, err := h.referralRepository.FindRecentPaginated(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]referralDTO, 0, len(refs))
	for _, ref := range refs {
		items = append(items, toReferralDTO(ref))
	}
	h.hydrateReferralUsernames(r.Context(), items)
	writeJSON(w, http.StatusOK, Page[referralDTO]{Items: items, Total: total, Page: page, Limit: limit})
}
