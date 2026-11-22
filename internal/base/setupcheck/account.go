package setupcheck

import (
	"encoding/csv"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The account half of the engine is a port of scripts/room-account.ps1. The wording and the rules are the script's, so
// the two cannot disagree about whether an account is a good one to run a room as.

// Facts are the probe's key=value lines: user, host, and elevated, sids, docker on Windows or uid, groups, sudo.* on Unix.
type Facts map[string]string

// Verdict is what the facts say. Reasons are the phrases that make the account a warn, in the order found.
type Verdict struct {
	User        string
	Reasons     []string
	Operator    bool
	OperatorWhy string
	Known       bool
}

var (
	domainAdmins = regexp.MustCompile(`^S-1-5-21-\d+-\d+-\d+-512$`)
	enterprise   = regexp.MustCompile(`^S-1-5-21-\d+-\d+-\d+-519$`)
	probeKeys    = map[string]bool{"user": true, "uid": true, "groups": true, "host": true, "elevated": true, "sids": true,
		"sudo.rc": true, "sudo.nopasswd": true, "sudo.all": true, "err": true}
)

// ParseFacts reads key=value lines. The first value of a key is the one taken, and one probe key said again with another
// value is not an answer: bad says so, and the caller must not read a verdict from it.
func ParseFacts(lines []string) (f Facts, bad string) {
	f = Facts{}
	for _, l := range lines {
		k, v, ok := strings.Cut(l, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		v = strings.TrimRight(v, " \r\t")
		if old, seen := f[k]; seen {
			if probeKeys[k] && old != v {
				return Facts{}, "the probe output was tampered with or noisy (a fact was said twice with two values)"
			}
			continue
		}
		f[k] = v
	}
	return f, ""
}

// WhoamiGroups reads `whoami /groups /fo csv /nh`. The SID is the third column and the same on every language, so the
// localized attribute column ("deny only") is never read: a listed SID is a member, even one UAC filtered. docker is the
// count of groups named docker-users, the one group matched by name because its RID differs on every machine.
func WhoamiGroups(out string) (sids []string, docker int) {
	r := csv.NewReader(strings.NewReader(out))
	r.FieldsPerRecord = -1
	rows, _ := r.ReadAll()
	for _, row := range rows {
		if len(row) < 3 || !strings.HasPrefix(row[2], "S-1-") {
			continue
		}
		sids = append(sids, row[2])
		if strings.EqualFold(row[0][strings.LastIndex(row[0], `\`)+1:], "docker-users") && strings.Contains(row[0], `\`) {
			docker++
		}
	}
	return sids, docker
}

// Elevated says the token carries the High or System integrity label, which is what an elevated admin token has.
func Elevated(sids []string) bool {
	for _, s := range sids {
		if s == "S-1-16-12288" || s == "S-1-16-16384" {
			return true
		}
	}
	return false
}

// OperatorNames splits the operator list: commas and semicolons, blanks dropped.
func OperatorNames(parts ...string) []string {
	var out []string
	for _, p := range parts {
		for _, n := range strings.FieldsFunc(p, func(r rune) bool { return r == ',' || r == ';' }) {
			if n = strings.TrimSpace(n); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

func shortName(n string) string {
	n = n[strings.LastIndex(n, `\`)+1:]
	n, _, _ = strings.Cut(n, ".")
	return strings.ToLower(n)
}

// Judge is a pure function of the facts. kind is windows, mac or linux.
func Judge(f Facts, kind string, operators []string) Verdict {
	user := f["user"]
	if user == "" {
		return Verdict{}
	}
	v := Verdict{User: user, Known: true}
	add := func(s string) { v.Reasons = append(v.Reasons, s) }
	if kind == "windows" {
		var sids []string
		for _, s := range strings.Split(f["sids"], ";") {
			if s != "" {
				sids = append(sids, s)
			}
		}
		if len(sids) == 0 {
			return Verdict{User: user}
		}
		has := func(want string) bool {
			for _, s := range sids {
				if s == want {
					return true
				}
			}
			return false
		}
		elev := f["elevated"] == "True"
		if has("S-1-5-32-544") {
			w := "is in Administrators (S-1-5-32-544)"
			if !elev {
				w += " with a filtered token"
			}
			add(w)
		}
		if elev {
			add("the token is elevated")
		}
		for _, s := range sids {
			if domainAdmins.MatchString(s) {
				add("is in Domain Admins (RID 512)")
				break
			}
		}
		for _, s := range sids {
			if enterprise.MatchString(s) {
				add("is in Enterprise Admins (RID 519)")
				break
			}
		}
		if has("S-1-5-32-551") {
			add("is in Backup Operators (S-1-5-32-551, can read every file)")
		}
		if has("S-1-5-32-578") {
			add("is in Hyper-V Administrators (S-1-5-32-578, can mount any disk)")
		}
		if n, _ := strconv.Atoi(f["docker"]); n > 0 {
			add("is in the group docker-users (Docker Desktop reaches host files)")
		}
	} else {
		if f["uid"] == "0" {
			add("is root (uid 0)")
		}
		admin, rootGroups := []string{"sudo", "wheel", "admin"}, []string{"docker", "lxd", "incus-admin", "libvirt"}
		if kind == "mac" {
			admin, rootGroups = []string{"admin", "wheel"}, nil
		}
		for _, g := range strings.Split(f["groups"], ",") {
			switch {
			case g == "":
			case contains(admin, g):
				add("is in the group " + g)
			case contains(rootGroups, g):
				add("is in the group " + g + " (root on this host)")
			}
		}
		switch rc := f["sudo.rc"]; {
		case rc == "0" && atoi(f["sudo.nopasswd"]) > 0:
			add("can run any command with sudo and no password (sudo -n -l says NOPASSWD: ALL)")
		case rc == "0" && atoi(f["sudo.all"]) > 0:
			add("can run any command with sudo (sudo -n -l lists ALL)")
		case rc == "0":
			add("can use sudo with no password for some command (sudo -n -l works), and one command is usually a way to root")
		case rc == "timeout":
			add("may have sudo, since sudo -n -l did not answer in 5 seconds")
		}
	}
	host := shortName(f["host"])
	for _, o := range operators {
		name, h, _ := strings.Cut(o, "@")
		same := strings.EqualFold(user[strings.LastIndex(user, `\`)+1:], name)
		if strings.Contains(name, `\`) {
			same = strings.EqualFold(user, name)
		}
		if same && (h == "" || shortName(h) == host) {
			v.Operator, v.OperatorWhy = true, "is listed as an operator account (ATRIUM_OPERATOR_ACCOUNT)"
			break
		}
	}
	return v
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func joinReasons(r []string) string {
	if len(r) <= 1 {
		return strings.Join(r, "")
	}
	return strings.Join(r[:len(r)-1], ", ") + " and " + r[len(r)-1]
}

const accountsPage = "see docs/room-accounts.md"

// AccountRows are account.standard and account.dedicated for one probe answer. probeErr is why the probe could not
// answer, "" when it did. A probe that cannot answer is a warn, never an ok.
func AccountRows(f Facts, probeErr, kind string, operators []string) (standard, dedicated Result) {
	standard = Result{ID: "account.standard", Label: "the room's account is a standard user"}
	dedicated = Result{ID: "account.dedicated", Label: "the room's account is not the operator's own"}
	v := Judge(f, kind, operators)
	if !v.Known {
		why := f["err"]
		if why == "" {
			why = probeErr
		}
		if why == "" {
			why = "the probe said nothing"
		}
		standard.State, standard.Detail = Warn, fmt.Sprintf("could not tell who the room runs as (%s). %s", why, accountsPage)
		dedicated.State, dedicated.Detail = Warn, standard.Detail
		return
	}
	if len(v.Reasons) == 0 {
		none := "not elevated, not in an admin group, not the operator's account"
		if kind != "windows" {
			none = "not elevated, not in an admin group, no passwordless sudo"
		}
		standard.State, standard.Detail = OK, v.User+": "+none
	} else {
		standard.State, standard.Fix = Warn, FixAdmin
		standard.Detail = fmt.Sprintf("%s %s. an agent in this room does whatever it is talked into with that account's rights. %s",
			v.User, joinReasons(v.Reasons), accountsPage)
	}
	switch {
	case len(operators) == 0:
		dedicated.State = NA
		dedicated.Detail = "no operator account is named. set ATRIUM_OPERATOR_ACCOUNT (name, DOMAIN\\name or name@host) to check this"
	case v.Operator:
		dedicated.State, dedicated.Fix = Warn, FixAdmin
		dedicated.Detail = fmt.Sprintf("%s %s, so the agents hold everything that account holds. %s", v.User, v.OperatorWhy, accountsPage)
	default:
		dedicated.State, dedicated.Detail = OK, v.User+" is not an operator account"
	}
	return
}

// probeSudo is the five second cap `sudo -n -l` gets: sudo can wait on the network, and a wait must read as a warn.
const probeSudo = 5 * time.Second
