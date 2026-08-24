package update

import (
	"strconv"
	"strings"
)

// DevBuild is the version an untagged build carries — `main.version` defaults
// to it and the Makefile only overrides it from a tag.
//
// It is never updated over the air. A developer's own build is newer than
// anything published, whatever the numbers say, and replacing it with a release
// would throw away the thing they are working on.
const DevBuild = "dev"

// ValidVersion reports whether s is a version this program will compare.
//
// A deliberately small subset of semver: three numbers, an optional prerelease,
// no build metadata. Build metadata is excluded because semver says it must be
// ignored when comparing, which would make two different downloads compare
// equal — an ambiguity worth refusing rather than resolving.
func ValidVersion(s string) bool {
	_, ok := parse(s)
	return ok
}

type semver struct {
	num [3]int
	pre []string
}

func parse(s string) (semver, bool) {
	var v semver
	// Not trimmed. A version reaches a menu-bar title and a filename, and
	// "0.2.0 " arriving from a manifest means something upstream is wrong;
	// tidying it up hides that. The "v" of a git tag is the one concession.
	s = strings.TrimPrefix(s, "v")
	if s == "" || strings.ContainsAny(s, "+ \t\n\r") {
		return v, false
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		// Not strconv.Atoi alone: it accepts "+1" and "-1", and leading zeros
		// are not a version number.
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return v, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return v, false
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v.num[i] = n
	}
	if hasPre {
		if pre == "" {
			return v, false
		}
		v.pre = strings.Split(pre, ".")
		for _, id := range v.pre {
			if id == "" {
				return v, false
			}
			for _, r := range id {
				if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
					return v, false
				}
			}
		}
	}
	return v, true
}

// Normalize pads a two-part version to three so it can be compared.
//
// macOS reports "15.6" from sw_vers, and a manifest may sensibly say a minimum
// of "12.0". Neither is semver, and refusing them would mean the minimum-OS
// field could not express the versions people actually write.
func Normalize(s string) (string, bool) {
	t := strings.TrimPrefix(strings.TrimSpace(s), "v")
	if strings.Count(t, ".") == 1 && !strings.Contains(t, "-") {
		t += ".0"
	}
	if !ValidVersion(t) {
		return "", false
	}
	return t, true
}

// Compare orders two versions: -1 if a is older, 0 if equal, 1 if a is newer.
// An unparseable version sorts before a valid one, so a version this build
// cannot understand is never treated as an upgrade.
func Compare(a, b string) int {
	va, aok := parse(a)
	vb, bok := parse(b)
	switch {
	case !aok && !bok:
		return 0
	case !aok:
		return -1
	case !bok:
		return 1
	}
	for i := 0; i < 3; i++ {
		if va.num[i] != vb.num[i] {
			if va.num[i] < vb.num[i] {
				return -1
			}
			return 1
		}
	}
	return comparePre(va.pre, vb.pre)
}

// comparePre implements the part of semver everyone forgets: a prerelease is
// older than the release it leads up to, so 0.3.0-dev.1 < 0.3.0.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		na, aNum := numeric(a[i])
		nb, bNum := numeric(b[i])
		switch {
		case aNum && bNum:
			if na < nb {
				return -1
			}
			return 1
		case aNum:
			// Numeric identifiers have lower precedence than alphanumeric.
			return -1
		case bNum:
			return 1
		case a[i] < b[i]:
			return -1
		default:
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func numeric(s string) (int, bool) {
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// Newer reports whether candidate is an upgrade from current.
//
// It answers no for a dev build, and no for a downgrade. The downgrade case is
// what happens when somebody on the dev channel switches back to release: the
// release channel is genuinely behind, and offering to move them backwards
// would be offering to throw away the newer build they already have.
func Newer(current, candidate string) bool {
	if current == DevBuild || current == "" {
		return false
	}
	if !ValidVersion(candidate) {
		return false
	}
	return Compare(candidate, current) > 0
}
