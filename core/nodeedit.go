package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Node editing, shared by the web dashboard (server PUT /api/nodes/{id})
// and the native front-ends. An edit is validated against the rebuilt
// config and rolled back when sing-box rejects it, so a bad edit can
// never persist.

// UpdateNode replaces one node's name and outbound. The outbound's
// "tag" key is dropped (tags are assigned at config build time) and
// its "type" becomes the node's protocol label.
func UpdateNode(store *Store, mgr *Manager, workDir, id, name string, outbound map[string]any) error {
	idx := -1
	for i := range store.Data.Nodes {
		if store.Data.Nodes[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("node not found")
	}
	if outbound == nil {
		return fmt.Errorf("outbound must be a JSON object")
	}
	if t, _ := outbound["type"].(string); t == "" {
		return fmt.Errorf("outbound.type is required")
	}
	delete(outbound, "tag") // internal tag is forced at config build

	// snapshot for rollback: a rejected edit must not persist
	old := store.Data.Nodes[idx]
	oldOutbound := map[string]any{}
	if raw, err := json.Marshal(old.Outbound); err == nil {
		_ = json.Unmarshal(raw, &oldOutbound)
	}
	rollback := func() {
		store.Data.Nodes[idx].Outbound = oldOutbound
		store.Data.Nodes[idx].Tag = old.Tag
		store.Data.Nodes[idx].Protocol = old.Protocol
		_ = store.Save()
		_ = Regen(store.Data, workDir)
	}

	store.Data.Nodes[idx].Outbound = outbound
	store.Data.Nodes[idx].Protocol, _ = outbound["type"].(string)
	if strings.TrimSpace(name) != "" {
		store.Data.Nodes[idx].Tag = strings.TrimSpace(name)
	}
	_ = store.Save()
	if err := Regen(store.Data, workDir); err != nil {
		rollback()
		return err
	}
	// validate the rebuilt config so a bad edit is reported immediately
	// (skipped silently when no sing-box binary exists yet)
	if out, err := mgr.Check(); err != nil {
		var execErr *exec.Error
		if !errors.As(err, &execErr) {
			rollback()
			return fmt.Errorf("edit rejected by sing-box (rolled back): %v %s", err, out)
		}
	}
	return nil
}

// DeleteNode removes one node from the store (and its cached health
// entries are the caller's concern). Returns false when not found.
func DeleteNode(store *Store, id string) bool {
	kept := store.Data.Nodes[:0]
	found := false
	for _, n := range store.Data.Nodes {
		if n.ID == id {
			found = true
			continue
		}
		kept = append(kept, n)
	}
	if !found {
		return false
	}
	store.Data.Nodes = kept
	_ = store.Save()
	return true
}
