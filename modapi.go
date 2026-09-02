package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Read-only Modrinth / CurseForge API queries shared by the TUI (live add-mod
// search) and the CLI (`search`, `mod-info` — the agent's lookup tools). All
// requests go through a small disk cache so the two never hammer the APIs for
// the same data. CurseForge uses CURSEFORGE_API_KEY against the official API
// when set, and falls back to the keyless curse.tools mirror otherwise.

var modAPIClient = &http.Client{Timeout: 20 * time.Second}

var errAPINotFound = fmt.Errorf("not found")

// ── Disk cache ───────────────────────────────────────────────────────────────

const apiCacheTTL = 15 * time.Minute

func apiCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "packwiz-tui", "api")
}

// cachedGET fetches a URL with the given headers, serving from the disk cache
// while the entry is younger than apiCacheTTL. Only 200 responses are cached.
func cachedGET(reqURL string, headers map[string]string) ([]byte, error) {
	dir := apiCacheDir()
	var path string
	if dir != "" {
		sum := sha1.Sum([]byte(reqURL))
		path = filepath.Join(dir, hex.EncodeToString(sum[:]))
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < apiCacheTTL {
			if data, err := os.ReadFile(path); err == nil {
				return data, nil
			}
		}
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "packwiz-tui (github.com/flashgnash/packwiz-tui)")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := modAPIClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errAPINotFound
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if path != "" {
		os.MkdirAll(dir, 0755)
		os.WriteFile(path, data, 0644)
	}
	return data, nil
}

func modrinthGet(path string, q url.Values, into any) error {
	u := "https://api.modrinth.com/v2" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	data, err := cachedGET(u, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

func curseforgeGet(path string, q url.Values, into any) error {
	key := os.Getenv("CURSEFORGE_API_KEY")
	base := "https://api.curse.tools/v1/cf"
	var headers map[string]string
	if key != "" {
		base = "https://api.curseforge.com/v1"
		headers = map[string]string{"x-api-key": key}
	}
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	data, err := cachedGET(u, headers)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

// Site logos for the add-mod popup's source badges (kitty graphics only).
// Google's favicon service serves both as PNG at a stable URL.
const (
	modrinthLogoURL   = "https://www.google.com/s2/favicons?domain=modrinth.com&sz=64"
	curseforgeLogoURL = "https://www.google.com/s2/favicons?domain=curseforge.com&sz=64"
)

// cfLoaderTypes maps loader names to CurseForge's modLoaderType enum.
var cfLoaderTypes = map[string]string{
	"forge": "1", "fabric": "4", "quilt": "5", "neoforge": "6",
}

// ── Search ───────────────────────────────────────────────────────────────────

// ModRef is a mod's identity on one platform.
type ModRef struct {
	ProjectID string
	Slug      string
}

// ModHit is one search result, possibly merged across both sources.
type ModHit struct {
	Source      string // primary source — where enter installs from by default
	Slug        string
	Title       string
	Description string
	Downloads   float64
	ProjectID   string
	IconURL     string
	Gallery     []string          // screenshot URLs, featured first
	Refs        map[string]ModRef // per-source identity (install ids, page slugs)
}

// Sources lists the platforms hosting this hit, primary first.
func (h ModHit) Sources() []string {
	out := []string{h.Source}
	for _, s := range []string{"modrinth", "curseforge"} {
		if s != h.Source {
			if _, ok := h.Refs[s]; ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// SearchModrinth searches Modrinth mods, filtered by mc version and loader
// when non-empty.
func SearchModrinth(query, mc, loader string, limit int) ([]ModHit, error) {
	facets := [][]string{{"project_type:mod"}}
	if mc != "" {
		facets = append(facets, []string{"versions:" + mc})
	}
	if loader != "" {
		facets = append(facets, []string{"categories:" + loader})
	}
	fjson, _ := json.Marshal(facets)
	q := url.Values{
		"query":  {query},
		"limit":  {fmt.Sprint(limit)},
		"facets": {string(fjson)},
	}
	var res struct {
		Hits []struct {
			Slug            string   `json:"slug"`
			Title           string   `json:"title"`
			Description     string   `json:"description"`
			Downloads       float64  `json:"downloads"`
			ProjectID       string   `json:"project_id"`
			IconURL         string   `json:"icon_url"`
			Gallery         []string `json:"gallery"`
			FeaturedGallery string   `json:"featured_gallery"`
		} `json:"hits"`
	}
	if err := modrinthGet("/search", q, &res); err != nil {
		return nil, err
	}
	hits := make([]ModHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		gallery := h.Gallery
		if h.FeaturedGallery != "" {
			gallery = append([]string{h.FeaturedGallery}, gallery...)
		}
		hits = append(hits, ModHit{
			Source: "modrinth", Slug: h.Slug, Title: h.Title,
			Description: h.Description, Downloads: h.Downloads, ProjectID: h.ProjectID,
			IconURL: h.IconURL, Gallery: gallery,
			Refs: map[string]ModRef{"modrinth": {ProjectID: h.ProjectID, Slug: h.Slug}},
		})
	}
	return hits, nil
}

type cfMod struct {
	ID            float64 `json:"id"`
	Slug          string  `json:"slug"`
	Name          string  `json:"name"`
	Summary       string  `json:"summary"`
	DownloadCount float64 `json:"downloadCount"`
	Logo          struct {
		ThumbnailURL string `json:"thumbnailUrl"`
	} `json:"logo"`
	Screenshots []struct {
		URL string `json:"url"`
	} `json:"screenshots"`
}

func (m cfMod) hit() ModHit {
	var gallery []string
	for _, s := range m.Screenshots {
		if s.URL != "" {
			gallery = append(gallery, s.URL)
		}
	}
	id := fmt.Sprintf("%.0f", m.ID)
	return ModHit{
		Source: "curseforge", Slug: m.Slug, Title: m.Name,
		Description: m.Summary, Downloads: m.DownloadCount,
		ProjectID: id,
		IconURL:   m.Logo.ThumbnailURL,
		Gallery:   gallery,
		Refs:      map[string]ModRef{"curseforge": {ProjectID: id, Slug: m.Slug}},
	}
}

// SearchCurseforge searches CurseForge mods (gameId 432 = Minecraft, classId
// 6 = mods), filtered by mc version and loader when non-empty.
func SearchCurseforge(query, mc, loader string, limit int) ([]ModHit, error) {
	q := url.Values{
		"gameId":       {"432"},
		"classId":      {"6"},
		"searchFilter": {query},
		"pageSize":     {fmt.Sprint(limit)},
		"sortField":    {"2"}, // popularity
		"sortOrder":    {"desc"},
	}
	if mc != "" {
		q.Set("gameVersion", mc)
	}
	if lt := cfLoaderTypes[loader]; lt != "" {
		q.Set("modLoaderType", lt)
	}
	var res struct {
		Data []cfMod `json:"data"`
	}
	if err := curseforgeGet("/mods/search", q, &res); err != nil {
		return nil, err
	}
	hits := make([]ModHit, 0, len(res.Data))
	for _, m := range res.Data {
		hits = append(hits, m.hit())
	}
	return hits, nil
}

// SearchBothSources queries both APIs concurrently and merges the results,
// sorted by downloads. A mod present on both sources becomes one hit tagged
// with both — Modrinth is kept as the install source (this project prefers
// Modrinth), with CurseForge in AltSources. An error is returned only when
// nothing came back at all; a single failing source degrades to the other's
// results.
func SearchBothSources(query, mc, loader string, limit int) ([]ModHit, error) {
	var mrHits, cfHits []ModHit
	var mrErr, cfErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); mrHits, mrErr = SearchModrinth(query, mc, loader, limit) }()
	go func() { defer wg.Done(); cfHits, cfErr = SearchCurseforge(query, mc, loader, limit) }()
	wg.Wait()

	// Cross-platform identity: slug when it matches, else normalised title
	// (the same mod often has slightly different slugs on each site).
	norm := func(s string) string {
		var b []rune
		for _, r := range strings.ToLower(s) {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b = append(b, r)
			}
		}
		return string(b)
	}
	var hits []ModHit
	index := map[string]int{}
	for _, h := range mrHits {
		index[h.Slug] = len(hits)
		index[norm(h.Title)] = len(hits)
		hits = append(hits, h)
	}
	for _, h := range cfHits {
		i, dup := index[h.Slug]
		if !dup {
			i, dup = index[norm(h.Title)]
		}
		if dup {
			hits[i].Refs["curseforge"] = h.Refs["curseforge"]
			if len(hits[i].Gallery) == 0 {
				hits[i].Gallery = h.Gallery
			}
			continue
		}
		index[h.Slug] = len(hits)
		hits = append(hits, h)
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Downloads > hits[j].Downloads })
	if len(hits) == 0 {
		if mrErr != nil {
			return nil, fmt.Errorf("modrinth: %w", mrErr)
		}
		if cfErr != nil {
			return nil, fmt.Errorf("curseforge: %w", cfErr)
		}
	}
	return hits, nil
}

// ── CLI output (agent tools) ─────────────────────────────────────────────────

// packSearchFilter returns the mc version and loader to filter API results
// by, or empties when anyVersion is set or pack.toml doesn't parse.
func packSearchFilter(packDir string, anyVersion bool, out io.Writer) (mc, loader string) {
	if anyVersion {
		return "", ""
	}
	meta, err := ParsePackMeta(packDir)
	if err != nil {
		fmt.Fprintf(out, "note: %v — searching without version/loader filter\n", err)
		return "", ""
	}
	return meta.Minecraft, meta.Loader
}

func searchScope(mc, loader string) string {
	if mc == "" {
		return "all versions"
	}
	return mc + " " + loader
}

// CLISearch prints search results from one or both sources for the agent.
func CLISearch(packDir, source, query string, limit int, anyVersion bool, out io.Writer) error {
	if source == "" {
		source = "all"
	}
	if source != "all" && source != "modrinth" && source != "curseforge" {
		return fmt.Errorf("unknown source %q (want modrinth, curseforge, or all)", source)
	}
	mc, loader := packSearchFilter(packDir, anyVersion, out)
	scope := searchScope(mc, loader)

	printHits := func(name string, hits []ModHit, err error) error {
		if err != nil {
			fmt.Fprintf(out, "%s (%s): error: %v\n", name, scope, err)
			return err
		}
		fmt.Fprintf(out, "%s (%s): %d result(s) for %q\n", name, scope, len(hits), query)
		for _, h := range hits {
			fmt.Fprintf(out, "  %-28s %s [%s downloads] — %s\n",
				h.Slug, h.Title, humanCount(h.Downloads), truncate(h.Description, 90))
		}
		return nil
	}

	var firstErr error
	if source != "curseforge" {
		hits, err := SearchModrinth(query, mc, loader, limit)
		if e := printHits("modrinth", hits, err); e != nil && firstErr == nil {
			firstErr = e
		}
	}
	if source != "modrinth" {
		hits, err := SearchCurseforge(query, mc, loader, limit)
		if e := printHits("curseforge", hits, err); e != nil && firstErr == nil {
			firstErr = e
		}
	}
	fmt.Fprintln(out, "install: packwiz modrinth add <slug> / packwiz curseforge add <slug>  ·  details: packwiz-tui mod-info <slug>")
	return firstErr
}

// CLIModInfo prints a project's details and installable versions. With no
// source it tries Modrinth first and falls back to CurseForge.
func CLIModInfo(packDir, source, slug string, limit int, anyVersion bool, out io.Writer) error {
	mc, loader := packSearchFilter(packDir, anyVersion, out)
	switch source {
	case "modrinth":
		return modrinthModInfo(slug, mc, loader, limit, out)
	case "curseforge":
		return curseforgeModInfo(slug, mc, loader, limit, out)
	case "", "all":
		if err := modrinthModInfo(slug, mc, loader, limit, out); err == nil || err != errAPINotFound {
			return err
		}
		fmt.Fprintf(out, "not on modrinth — trying curseforge\n")
		return curseforgeModInfo(slug, mc, loader, limit, out)
	}
	return fmt.Errorf("unknown source %q (want modrinth or curseforge)", source)
}

func modrinthModInfo(slug, mc, loader string, limit int, out io.Writer) error {
	var p struct {
		ID          string  `json:"id"`
		Slug        string  `json:"slug"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		ClientSide  string  `json:"client_side"`
		ServerSide  string  `json:"server_side"`
		Downloads   float64 `json:"downloads"`
	}
	if err := modrinthGet("/project/"+url.PathEscape(slug), nil, &p); err != nil {
		return err
	}
	fmt.Fprintf(out, "modrinth: %s (%s) — project-id %s\n", p.Title, p.Slug, p.ID)
	fmt.Fprintf(out, "  %s\n", truncate(p.Description, 300))
	fmt.Fprintf(out, "  downloads %s · client %s · server %s\n",
		humanCount(p.Downloads), p.ClientSide, p.ServerSide)

	q := url.Values{}
	if mc != "" {
		gv, _ := json.Marshal([]string{mc})
		q.Set("game_versions", string(gv))
	}
	if loader != "" {
		ld, _ := json.Marshal([]string{loader})
		q.Set("loaders", string(ld))
	}
	var vers []struct {
		ID            string `json:"id"`
		VersionNumber string `json:"version_number"`
		DatePublished string `json:"date_published"`
		Files         []struct {
			Filename string `json:"filename"`
		} `json:"files"`
	}
	if err := modrinthGet("/project/"+url.PathEscape(slug)+"/version", q, &vers); err != nil {
		return err
	}
	shown := minInt(len(vers), limit)
	fmt.Fprintf(out, "versions for %s (newest first, %d of %d):\n", searchScope(mc, loader), shown, len(vers))
	for _, v := range vers[:shown] {
		file := ""
		if len(v.Files) > 0 {
			file = v.Files[0].Filename
		}
		date := v.DatePublished
		if len(date) > 10 {
			date = date[:10]
		}
		fmt.Fprintf(out, "  %-24s version-id %-10s %s  %s\n", v.VersionNumber, v.ID, date, file)
	}
	fmt.Fprintf(out, "install latest: packwiz modrinth add %s\n", p.Slug)
	fmt.Fprintf(out, "install specific: packwiz modrinth add --project-id %s --version-id <id>\n", p.ID)
	return nil
}

func curseforgeModInfo(slug, mc, loader string, limit int, out io.Writer) error {
	q := url.Values{"gameId": {"432"}, "classId": {"6"}, "slug": {slug}}
	var res struct {
		Data []cfMod `json:"data"`
	}
	if err := curseforgeGet("/mods/search", q, &res); err != nil {
		return err
	}
	if len(res.Data) == 0 {
		return errAPINotFound
	}
	m := res.Data[0]
	id := fmt.Sprintf("%.0f", m.ID)
	fmt.Fprintf(out, "curseforge: %s (%s) — mod-id %s\n", m.Name, m.Slug, id)
	fmt.Fprintf(out, "  %s\n", truncate(m.Summary, 300))
	fmt.Fprintf(out, "  downloads %s\n", humanCount(m.DownloadCount))

	fq := url.Values{"pageSize": {fmt.Sprint(limit)}}
	if mc != "" {
		fq.Set("gameVersion", mc)
	}
	if lt := cfLoaderTypes[loader]; lt != "" {
		fq.Set("modLoaderType", lt)
	}
	var fres struct {
		Data []struct {
			ID       float64 `json:"id"`
			FileName string  `json:"fileName"`
			FileDate string  `json:"fileDate"`
		} `json:"data"`
	}
	if err := curseforgeGet("/mods/"+id+"/files", fq, &fres); err != nil {
		return fmt.Errorf("listing files: %w", err)
	}
	fmt.Fprintf(out, "files for %s (newest first, %d shown):\n", searchScope(mc, loader), len(fres.Data))
	for _, f := range fres.Data {
		date := f.FileDate
		if len(date) > 10 {
			date = date[:10]
		}
		fmt.Fprintf(out, "  file-id %-10.0f %s  %s\n", f.ID, date, f.FileName)
	}
	fmt.Fprintf(out, "install latest: packwiz curseforge add %s\n", m.Slug)
	fmt.Fprintf(out, "install specific: packwiz curseforge add --addon-id %s --file-id <id>\n", id)
	return nil
}

// humanCount renders a download count compactly (1.2M, 33k).
func humanCount(n float64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1fB", n/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.0fk", n/1e3)
	}
	return fmt.Sprintf("%.0f", n)
}
