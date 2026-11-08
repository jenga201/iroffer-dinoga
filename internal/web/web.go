package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"

	"github.com/jenga201/dinoga/internal/packs"
)

// Config controls the HTTP web interface.
type Config struct {
	// Allow is an optional list of CIDR networks. When non-empty, only clients
	// whose IP falls inside one of these networks are served; everyone else
	// gets 403. An empty Allow means "allow all".
	Allow []*net.IPNet
	// Deny is an optional list of CIDR networks. Clients whose IP falls inside
	// one of these networks are always rejected with 403, even when they also
	// match Allow.
	Deny []*net.IPNet
}

// ParseCIDRs parses CIDR strings into []*net.IPNet. A bare IP address (no "/")
// is treated as a single host (/32 for IPv4, /128 for IPv6). It returns an
// error if any entry is malformed.
func ParseCIDRs(in []string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) == "" {
			continue
		}
		cidr := strings.TrimSpace(s)
		if !strings.Contains(cidr, "/") {
			if ip := net.ParseIP(cidr); ip != nil {
				if ip.To4() != nil {
					cidr += "/32"
				} else {
					cidr += "/128"
				}
			}
		}
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("web: parse CIDR %q: %w", s, err)
		}
		out = append(out, n)
	}
	return out, nil
}

type handler struct {
	packs func() []packs.Pack
	cfg   Config
	tmpl  *template.Template
}

type listView struct {
	Query string
	Packs []packView
}

type packView struct {
	Number int
	Name   string
	Size   int64
}

type packJSON struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
}

// New builds the HTTP handler. packList is invoked once per request to obtain
// the current pack listing; it may be nil, in which case the listing is empty.
// cfg carries optional IP allow/deny CIDR lists.
func New(packList func() []packs.Pack, cfg Config) http.Handler {
	if packList == nil {
		packList = func() []packs.Pack { return nil }
	}
	h := &handler{
		packs: packList,
		cfg:   cfg,
		tmpl: template.Must(template.New("packs").Funcs(template.FuncMap{
			"size": humanSize,
		}).Parse(packsHTML)),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.serveList)
	mux.HandleFunc("GET /search", h.serveSearch)
	mux.HandleFunc("GET /packlist.txt", h.servePacklistTxt)
	mux.HandleFunc("GET /list.json", h.serveJSON)
	mux.HandleFunc("GET /md5/{name}", h.serveMD5)
	mux.HandleFunc("GET /sfv", h.serveSFV)
	mux.HandleFunc("GET /dl/{name}", h.serveDownload)

	return h.authorize(mux)
}

// authorize wraps next with the IP allow/deny policy.
func (h *handler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.allowed(r) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allowed reports whether the client behind r may access the interface.
func (h *handler) allowed(r *http.Request) bool {
	ip := clientIP(r)
	if ip == nil {
		// No readable address: deny if any policy is configured.
		return len(h.cfg.Allow) == 0 && len(h.cfg.Deny) == 0
	}
	for _, n := range h.cfg.Deny {
		if n.Contains(ip) {
			return false
		}
	}
	if len(h.cfg.Allow) == 0 {
		return true
	}
	for _, n := range h.cfg.Allow {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP extracts the client IP from r.RemoteAddr (host only).
func clientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

func (h *handler) serveList(w http.ResponseWriter, r *http.Request) {
	h.renderList(w, "")
}

func (h *handler) serveSearch(w http.ResponseWriter, r *http.Request) {
	h.renderList(w, r.URL.Query().Get("q"))
}

func (h *handler) renderList(w http.ResponseWriter, q string) {
	list := h.filter(h.packs(), q)
	data := listView{
		Query: q,
		Packs: make([]packView, 0, len(list)),
	}
	for _, p := range list {
		data.Packs = append(data.Packs, packView{Number: p.Number, Name: p.Name, Size: p.Size})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.tmpl.Execute(w, data)
}

// servePacklistTxt emits a plain-text pack listing, one pack per line, in the
// XDCC list line format. It is the endpoint advertised in the bot's
// "xdcc list" reply (http://<ip>:<port>/packlist.txt).
func (h *handler) servePacklistTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, p := range h.packs() {
		fmt.Fprintf(w, "#%d %d %s\n", p.Number, p.Size, p.Name)
	}
}

func (h *handler) serveJSON(w http.ResponseWriter, r *http.Request) {
	list := h.packs()
	out := make([]packJSON, 0, len(list))
	for _, p := range list {
		out = append(out, packJSON{Number: p.Number, Name: p.Name, Size: p.Size})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (h *handler) serveMD5(w http.ResponseWriter, r *http.Request) {
	p, ok := h.findByName(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, p.MD5())
}

func (h *handler) serveSFV(w http.ResponseWriter, r *http.Request) {
	var sb strings.Builder
	for _, p := range h.packs() {
		fmt.Fprintf(&sb, "%s %08X\r\n", p.Name, p.CRC32())
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, sb.String())
}

func (h *handler) serveDownload(w http.ResponseWriter, r *http.Request) {
	p, ok := h.findByName(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, p.Path)
}

func (h *handler) findByName(name string) (packs.Pack, bool) {
	for _, p := range h.packs() {
		if p.Name == name {
			return p, true
		}
	}
	return packs.Pack{}, false
}

// filter returns the pack listing filtered by a case-insensitive substring
// match on Name. An empty query returns the listing unchanged.
func (h *handler) filter(in []packs.Pack, q string) []packs.Pack {
	if q == "" {
		return in
	}
	q = strings.ToLower(q)
	out := make([]packs.Pack, 0, len(in))
	for _, p := range in {
		if strings.Contains(strings.ToLower(p.Name), q) {
			out = append(out, p)
		}
	}
	return out
}

// humanSize renders a byte count in a compact, human-readable form.
func humanSize(n int64) string {
	const unit = int64(1024)
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	units := "KMGTPE"
	div := unit
	exp := 0
	for m := n / unit; m >= unit && exp < len(units)-1; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), units[exp])
}

const packsHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Pack list</title>
</head>
<body>
<h1>Pack list</h1>
{{if .Query}}<p>Filter: {{.Query}}</p>{{end}}
<table>
<thead><tr><th>#</th><th>Size</th><th>Name</th></tr></thead>
<tbody>
{{range .Packs}}
<tr><td>{{.Number}}</td><td>{{size .Size}}</td><td>{{.Name}}</td></tr>
{{end}}
</tbody>
</table>
</body>
</html>
`
