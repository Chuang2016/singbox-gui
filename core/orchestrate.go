package core

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Shared start/refresh orchestration for both front-ends. These
// functions used to be methods on server.Server (regen, refreshSub,
// the /api/rulesets/refresh handler), which made them web-only.

// Regen makes sure every rule-set pack the config references is
// downloaded, then rebuilds config.json. Rule-sets are fetched via
// mirrors directly (no proxy needed).
func Regen(d StoreData, workDir string) error {
	if d.Cfg.Mode == "bypass-cn" {
		if err := EnsureRuleSets(workDir); err != nil {
			return err
		}
	}
	if d.Cfg.BlockAds {
		if err := EnsureExtraRuleSets(workDir, []string{"geosite-category-ads-all"}); err != nil {
			return err
		}
	}
	if d.Cfg.GFWList {
		if err := EnsureGFWList(workDir); err != nil {
			return err
		}
	}
	_, err := BuildConfig(d, workDir)
	return err
}

// RefreshSubscription refetches one subscription, replacing its nodes,
// and returns how many nodes it now provides.
func RefreshSubscription(store *Store, id string) (int, error) {
	var sub *Subscription
	for i := range store.Data.Subs {
		if store.Data.Subs[i].ID == id {
			sub = &store.Data.Subs[i]
			break
		}
	}
	if sub == nil {
		return 0, fmt.Errorf("subscription not found")
	}
	obs, tags, protos, err := FetchSubscription(sub.URL)
	if err != nil {
		return 0, err
	}
	// drop old nodes from this sub
	kept := store.Data.Nodes[:0]
	for _, n := range store.Data.Nodes {
		if n.Source != "sub:"+id {
			kept = append(kept, n)
		}
	}
	store.Data.Nodes = kept
	for i, ob := range obs {
		store.Data.Nodes = append(store.Data.Nodes, Node{
			ID: NewID("n"), Tag: tags[i], Protocol: protos[i],
			Outbound: ob, Source: "sub:" + id,
		})
	}
	sub.Count = len(obs)
	sub.UpdatedAt = time.Now().Format("2006-01-02 15:04")
	_ = store.Save()
	return len(obs), nil
}

// CreateSubscription appends a new (empty) subscription and saves.
// Callers usually RefreshSubscription right after.
func CreateSubscription(store *Store, name, url string) Subscription {
	sub := Subscription{ID: NewID("sub"), Name: name, URL: url}
	store.Data.Subs = append(store.Data.Subs, sub)
	_ = store.Save()
	return sub
}

// DeleteSubscription removes a subscription and all of its nodes.
func DeleteSubscription(store *Store, id string) {
	kept := store.Data.Subs[:0]
	for _, sb := range store.Data.Subs {
		if sb.ID != id {
			kept = append(kept, sb)
		}
	}
	store.Data.Subs = kept
	kn := store.Data.Nodes[:0]
	for _, n := range store.Data.Nodes {
		if n.Source != "sub:"+id {
			kn = append(kn, n)
		}
	}
	store.Data.Nodes = kn
	_ = store.Save()
}

// RuleSetFiles are the downloaded pack files managed as a set.
var RuleSetFiles = []string{"geosite-cn.srs", "geoip-cn.srs", "geosite-category-ads-all.srs", "gfwlist.json"}

// RefreshRuleSets force re-downloads the rule-set packs (bypassing
// the 7-day cache) and rebuilds the config via regen. Files are backed
// up first and restored if anything fails, so a network outage can
// never leave the install without usable rule-sets.
func RefreshRuleSets(workDir string, regen func() error) error {
	dir := RuleSetDir(workDir)
	for _, f := range RuleSetFiles {
		_ = os.Rename(filepath.Join(dir, f), filepath.Join(dir, f+".bak"))
	}
	if err := regen(); err != nil {
		for _, f := range RuleSetFiles { // restore backups
			_ = os.Rename(filepath.Join(dir, f+".bak"), filepath.Join(dir, f))
		}
		return err
	}
	for _, f := range RuleSetFiles {
		_ = os.Remove(filepath.Join(dir, f+".bak"))
	}
	return nil
}
