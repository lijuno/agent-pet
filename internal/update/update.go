// Package update models the update manifest and what the pet knows about a
// newer version. It is deliberately inert: no network, no subprocess, no
// filesystem.
//
// That is the whole point of the package boundary. petd links this in so it can
// hold and report an update someone told it about, and SECURITY.md's claim that
// the daemon never dials out survives because there is nothing here to dial
// with. The fetching, downloading and installing live in cmd/petctl, a separate
// program petd cannot import.
//
// Everything crossing a trust boundary is validated here rather than at the
// call sites: a manifest is a file on a web server, and a Status arrives over
// an unauthenticated loopback API that any local process may post to. §26 — a
// version string ends up in a menu-bar title and a URL ends up in a browser, so
// neither is taken on trust.
package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Repo is the only repository an update may come from. Pinning it here means a
// manifest — which is fetched over the network and may be served by anyone who
// controls that URL — cannot redirect the download somewhere else.
const Repo = "lijuno/agent-pet"

// IssuesURL is where a bug report goes. Built from Repo so the tracker cannot
// drift away from the repository the updates come from, and so it satisfies
// ValidateNotesURL — the pet opens no URL that has not been through that.
const IssuesURL = "https://github.com/" + Repo + "/issues"

// MaxAssetSize bounds a download. The bundle is around 30 MB; this is loose
// enough never to bite and tight enough that a hostile manifest cannot ask the
// updater to fill the disk.
const MaxAssetSize = 500 << 20

// Channel is which stream of builds a user follows.
//
// A closed set, not free text: the channel becomes a path segment in the
// manifest URL, and a channel read from a config file or posted to the event
// API must not be able to reach for another path.
type Channel string

const (
	// Release is what everyone gets by default: versions the author has
	// deliberately published to everybody.
	Release Channel = "release"
	// Dev carries prereleases. Following it means being the person who finds
	// out first when something is broken.
	Dev Channel = "dev"
)

// Channels is every valid channel, in the order the menu should list them.
var Channels = []Channel{Release, Dev}

// ParseChannel accepts a channel name from a config file, a command line or the
// event API. Unknown names are rejected rather than defaulted, so a typo is
// reported instead of silently moving somebody between streams.
func ParseChannel(s string) (Channel, bool) {
	switch Channel(strings.TrimSpace(strings.ToLower(s))) {
	case Release:
		return Release, true
	case Dev:
		return Dev, true
	}
	return "", false
}

// Label is the channel as the menu bar spells it.
func (c Channel) Label() string {
	switch c {
	case Dev:
		return "Dev"
	default:
		return "Release"
	}
}

// Manifest is what a channel's JSON file says. It is written by
// scripts/release.sh from the artifact it just notarized, and published by a
// separate, deliberate commit — see docs/adr/0008-over-the-air-updates.md.
type Manifest struct {
	// Channel the file claims to be. Checked against the channel that was
	// asked for, so release.json cannot quietly serve dev builds.
	Channel  string `json:"channel"`
	Version  string `json:"version"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	MinMacOS string `json:"min_macos,omitempty"`
	// Published is a date, not a timestamp: releases here are cut by hand and
	// the hour is noise.
	Published string `json:"published,omitempty"`
	NotesURL  string `json:"notes_url,omitempty"`
}

// ParseManifest decodes and validates a manifest. Unknown fields are refused so
// a future field cannot be silently ignored by an old client that would then
// act on a manifest it only half understood.
func ParseManifest(data []byte, want Channel) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("manifest is not valid JSON: %w", err)
	}
	if err := m.validate(want); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) validate(want Channel) error {
	if m.Channel != "" {
		got, ok := ParseChannel(m.Channel)
		if !ok {
			return fmt.Errorf("manifest names an unknown channel %q", m.Channel)
		}
		if want != "" && got != want {
			return fmt.Errorf("asked for the %s channel and got a manifest for %s", want, got)
		}
	}
	if !ValidVersion(m.Version) {
		return fmt.Errorf("manifest version %q is not a version number", m.Version)
	}
	if err := ValidateAssetURL(m.URL); err != nil {
		return err
	}
	if !validSHA256(m.SHA256) {
		return fmt.Errorf("manifest sha256 %q is not 64 hex characters", m.SHA256)
	}
	if m.Size <= 0 || m.Size > MaxAssetSize {
		return fmt.Errorf("manifest size %d is not a plausible bundle size", m.Size)
	}
	if m.NotesURL != "" {
		if err := ValidateNotesURL(m.NotesURL); err != nil {
			return err
		}
	}
	if m.MinMacOS != "" {
		if _, ok := Normalize(m.MinMacOS); !ok {
			return fmt.Errorf("manifest min_macos %q is not a version number", m.MinMacOS)
		}
	}
	return nil
}

// ValidateAssetURL is the rule that makes a manifest safe to act on: whoever
// serves the manifest still cannot choose where the download comes from.
//
// A GitHub release asset, in this repository, over HTTPS. Nothing else. The
// host is compared after parsing, never by prefix — "github.com.evil.test"
// starts with the right characters and is not GitHub.
func ValidateAssetURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("manifest url %q is not a URL", raw)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("manifest url %q is not https", raw)
	}
	if u.Host != "github.com" {
		return fmt.Errorf("manifest url host %q is not github.com", u.Host)
	}
	want := "/" + Repo + "/releases/download/"
	if !strings.HasPrefix(u.Path, want) {
		return fmt.Errorf("manifest url %q is not a release asset of %s", raw, Repo)
	}
	if strings.Contains(u.Path, "..") {
		return fmt.Errorf("manifest url %q contains a path traversal", raw)
	}
	return nil
}

// ValidateNotesURL guards the one string in a manifest that a person is invited
// to click. It reaches the menu bar and, from there, a browser.
func ValidateNotesURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("notes url %q is not a URL", raw)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("notes url %q is not https", raw)
	}
	if u.Host != "github.com" {
		return fmt.Errorf("notes url host %q is not github.com", u.Host)
	}
	if !strings.HasPrefix(u.Path, "/"+Repo+"/") {
		return fmt.Errorf("notes url %q is not in %s", raw, Repo)
	}
	return nil
}

// ManifestURL fills the {channel} placeholder in a template. The channel is a
// Channel and not a string, so nothing user-supplied can become a path segment.
func ManifestURL(template string, c Channel) (string, error) {
	if _, ok := ParseChannel(string(c)); !ok {
		return "", fmt.Errorf("unknown channel %q", c)
	}
	raw := strings.ReplaceAll(template, "{channel}", string(c))
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("manifest url %q is not a URL", raw)
	}
	// The host is the user's own choice — a fork publishes its own manifest —
	// but plain HTTP is not, because the manifest is what decides whether an
	// update happens at all.
	if u.Scheme != "https" {
		return "", errors.New("the update manifest URL must be https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("manifest url %q has no host", raw)
	}
	return u.String(), nil
}

// Status is what petd holds and reports: the answer to "is there a newer
// version", as told to it by petctl. petd never works this out for itself.
type Status struct {
	Channel Channel `json:"channel"`
	// Current is the version of the running app, echoed back so a caller can
	// see what the comparison was made against.
	Current string `json:"current"`
	// Latest is the newest version the channel offers, empty if the check has
	// not run or found nothing.
	Latest    string    `json:"latest,omitempty"`
	Available bool      `json:"available"`
	NotesURL  string    `json:"notes_url,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitempty"`
	// Error is what went wrong with the last check, if anything. Reported
	// rather than swallowed: "no update" and "could not find out" are
	// different answers and the pet does not guess between them.
	Error string `json:"error,omitempty"`
}

// Validate is applied to a Status arriving over the event API. Any local
// process can post one, and the result is displayed in a menu — so the version
// must look like a version and the URL must be one worth opening.
func (s *Status) Validate() error {
	if s.Channel != "" {
		c, ok := ParseChannel(string(s.Channel))
		if !ok {
			return fmt.Errorf("unknown channel %q", s.Channel)
		}
		s.Channel = c
	}
	if s.Latest != "" && !ValidVersion(s.Latest) {
		return fmt.Errorf("latest %q is not a version number", s.Latest)
	}
	if s.Current != "" && !ValidVersion(s.Current) && s.Current != DevBuild {
		return fmt.Errorf("current %q is not a version number", s.Current)
	}
	if s.NotesURL != "" {
		if err := ValidateNotesURL(s.NotesURL); err != nil {
			return err
		}
	}
	if s.Available && s.Latest == "" {
		return errors.New("an update is available but no version was given")
	}
	// A free-text field that reaches a menu title. Bound it and strip anything
	// that is not text.
	s.Error = sanitiseLine(s.Error, 200)
	return nil
}

// sanitiseLine keeps a string printable and short. Control characters in a
// menu-bar title are at best ugly and at worst a way to make a menu say
// something other than what it appears to say.
func sanitiseLine(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		b.WriteRune(r)
		if b.Len() >= max {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func validSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
