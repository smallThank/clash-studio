package studio

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"clash-rule-gen/internal/config"
)

type providerNodesRequest struct {
	URL       string `json:"url"`
	UserAgent string `json:"userAgent,omitempty"`
	Refresh   bool   `json:"refresh,omitempty"`
}

type providerNodesResponse struct {
	Nodes     []string   `json:"nodes"`
	FetchedAt *time.Time `json:"fetchedAt,omitempty"`
}

func (s *Server) providerNodes(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	var req providerNodesRequest
	if err := parseBody(w, r, &req); err != nil {
		return err
	}
	source, _, err := subscriptionSource(strings.TrimSpace(req.URL), strings.TrimSpace(req.UserAgent))
	if err != nil {
		return err
	}
	ctx := r.Context()
	key := digest(source.URL + "\x00" + source.UserAgent)
	purpose := "provider-nodes:" + user.ID + ":" + hex.EncodeToString(key)
	if req.Refresh {
		if !s.limits.allow("import:"+user.ID, 24, 5*time.Minute) {
			return throttled()
		}
		startedAt := time.Now().UnixNano()
		rawYAML, err := s.fetchNodeSubscription(ctx, source)
		if err != nil {
			return err
		}
		doc, err := config.ParseImport(rawYAML, "nodes")
		if err != nil {
			return invalidConfig(err.Error(), nil)
		}
		nodes, err := json.Marshal(doc.ProxyNames())
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO provider_node_cache(owner_id, source_digest, sealed_nodes, fetched_at)
			VALUES(?, ?, ?, ?)
			ON CONFLICT(owner_id, source_digest) DO UPDATE SET
				sealed_nodes = excluded.sealed_nodes, fetched_at = excluded.fetched_at
			WHERE excluded.fetched_at >= provider_node_cache.fetched_at`,
			user.ID, key, s.seal(string(nodes), purpose), startedAt)
		if err != nil {
			return err
		}
	}

	var response providerNodesResponse
	var sealed []byte
	var fetchedAt int64
	err = s.db.QueryRowContext(ctx, `
		SELECT sealed_nodes, fetched_at FROM provider_node_cache
		WHERE owner_id = ? AND source_digest = ?`, user.ID, key).Scan(&sealed, &fetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, response)
		return nil
	}
	if err != nil {
		return err
	}
	nodes, err := s.unseal(sealed, purpose)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(nodes), &response.Nodes); err != nil {
		return err
	}
	fetched := time.Unix(0, fetchedAt).UTC()
	response.FetchedAt = &fetched
	writeJSON(w, http.StatusOK, response)
	return nil
}
