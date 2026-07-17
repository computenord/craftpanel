package mc

import (
	"context"
	"errors"
	"log"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/dns"
	"github.com/computenord/craftpanel/internal/fsutil"
)

// Domain mapping: with a base domain of mc.example.com every server is
// reachable as <id>.mc.example.com. A wildcard record (*.mc.example.com)
// points at this host — either created by the operator or managed by the
// panel through the Cloudflare API, which additionally maintains SRV
// records so Java players never have to type a port.

const DNSProviderCloudflare = "cloudflare"

var (
	ErrDNSNotConfigured = errors.New("configure a base domain and a Cloudflare API token first")
	ErrDNSBusy          = errors.New("a DNS sync is already running")

	domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)
)

// DNSStatus is the outcome of the last DNS reconciliation, shown in the
// panel settings.
type DNSStatus struct {
	LastSync int64    `json:"lastSync,omitempty"` // unix milliseconds
	OK       bool     `json:"ok"`
	Error    string   `json:"error,omitempty"`
	Records  int      `json:"records,omitempty"`
	Target   string   `json:"target,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// DomainRequest updates the domain mapping configuration. Nil fields keep
// their current value; an empty Token keeps the stored token (switch the
// provider to manual to drop it).
type DomainRequest struct {
	Domain   *string
	Provider *string
	Target   *string
	Token    *string
}

// normalizeDomain lowercases and strips "*." and trailing dots. Empty input
// turns the feature off.
func normalizeDomain(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	d = strings.TrimPrefix(d, "*.")
	d = strings.TrimSuffix(d, ".")
	if d == "" {
		return "", nil
	}
	if len(d) > 253 || !domainRe.MatchString(d) {
		return "", errors.New("base domain must be a plain DNS name like mc.example.com")
	}
	return d, nil
}

func dnsConfigured(st PanelSettings) bool {
	return st.Domain != "" && st.DNSProvider == DNSProviderCloudflare && st.DNSToken != ""
}

// SetDomainSettings validates and persists the domain configuration, then
// rewrites the proxies' forced-hosts blocks and reconciles DNS in the
// background. Returns human readable warnings (restart reminders).
func (m *Manager) SetDomainSettings(req DomainRequest) ([]string, error) {
	m.mu.Lock()
	st := m.settings
	old := st
	if req.Domain != nil {
		d, err := normalizeDomain(*req.Domain)
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		st.Domain = d
	}
	if req.Provider != nil {
		p := strings.ToLower(strings.TrimSpace(*req.Provider))
		if p != "" && p != DNSProviderCloudflare {
			m.mu.Unlock()
			return nil, errors.New("unknown DNS provider")
		}
		st.DNSProvider = p
	}
	if req.Target != nil {
		tg := strings.ToLower(strings.TrimSpace(*req.Target))
		if tg != "" && net.ParseIP(tg) == nil && !domainRe.MatchString(tg) {
			m.mu.Unlock()
			return nil, errors.New("record target must be an IP address or a hostname")
		}
		st.DNSTarget = tg
	}
	if req.Token != nil && strings.TrimSpace(*req.Token) != "" {
		st.DNSToken = strings.TrimSpace(*req.Token)
	}
	if st.DNSProvider == "" {
		st.DNSToken = ""
	}
	if st.DNSProvider == DNSProviderCloudflare {
		if st.Domain == "" {
			m.mu.Unlock()
			return nil, errors.New("set a base domain first")
		}
		if st.DNSToken == "" {
			m.mu.Unlock()
			return nil, errors.New("a Cloudflare API token is required")
		}
	}
	m.settings = st
	err := fsutil.WriteJSONAtomic(m.settingsPath, m.settings)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}

	warnings := []string{}
	if st.Domain != old.Domain {
		warnings = m.rewriteAllForcedHosts(st.Domain)
	}
	if dnsConfigured(old) && !dnsConfigured(st) {
		// The feature was switched off; remove the records the panel created.
		go m.cleanupDNS(old)
	}
	m.TriggerDNSSync("settings changed")
	return warnings, nil
}

// domainSnapshot captures everything address decoration and record
// generation need: the base domain and which backend sits behind which
// proxy port.
type domainSnapshot struct {
	domain     string
	dnsManaged bool
	proxyPort  map[string]int // backend id -> port of its Velocity proxy
}

func (m *Manager) domainSnapshot() domainSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.domainSnapshotLocked()
}

// domainSnapshotLocked is domainSnapshot for callers already holding m.mu.
func (m *Manager) domainSnapshotLocked() domainSnapshot {
	snap := domainSnapshot{
		domain:     m.settings.Domain,
		dnsManaged: dnsConfigured(m.settings),
		proxyPort:  map[string]int{},
	}
	if snap.domain == "" {
		return snap
	}
	for _, srv := range m.items {
		srv.mu.Lock()
		if srv.meta.Type == TypeVelocity {
			for _, id := range srv.meta.NetworkServers {
				if _, ok := snap.proxyPort[id]; !ok {
					snap.proxyPort[id] = srv.meta.Port
				}
			}
		}
		srv.mu.Unlock()
	}
	return snap
}

// decorateDomain fills the public hostname of a server when a base domain
// is configured. DomainPort is 0 when players can connect without a port:
// the effective port is the Java default, or SRV records cover it.
func decorateDomain(v *ServerView, snap domainSnapshot) {
	if snap.domain == "" {
		return
	}
	v.Domain = v.ID + "." + snap.domain
	port := v.Port
	if p, ok := snap.proxyPort[v.ID]; ok {
		port = p
	}
	switch {
	case v.Type == TypeBedrock:
		// Bedrock clients do not resolve SRV records.
		if port != bedrockBasePort {
			v.DomainPort = port
		}
	case snap.dnsManaged, port == basePort:
	default:
		v.DomainPort = port
	}
}

// desiredDNSRecords builds the record set for the configured domain: the
// wildcard pointing at target plus one SRV per Java server. Backends behind
// a Velocity proxy get the proxy's port so players land on the proxy, which
// routes them by hostname (forced hosts).
func (m *Manager) desiredDNSRecords(domain, target string) []dns.Record {
	recType := "CNAME"
	if ip := net.ParseIP(target); ip != nil {
		recType = "A"
		if ip.To4() == nil {
			recType = "AAAA"
		}
	}
	records := []dns.Record{{Type: recType, Name: "*." + domain, Content: target}}

	snap := m.domainSnapshot()
	m.mu.Lock()
	servers := make([]*Server, 0, len(m.items))
	for _, s := range m.items {
		servers = append(servers, s)
	}
	m.mu.Unlock()
	for _, srv := range servers {
		srv.mu.Lock()
		id, typ, port := srv.meta.ID, srv.meta.Type, srv.meta.Port
		srv.mu.Unlock()
		if typ == TypeBedrock {
			continue
		}
		if p, ok := snap.proxyPort[id]; ok {
			port = p
		}
		records = append(records, dns.Record{
			Type: "SRV", Name: "_minecraft._tcp." + id + "." + domain,
			Port: port, Target: id + "." + domain,
		})
	}
	return records
}

// DNSStatus returns the outcome of the last reconciliation run.
func (m *Manager) DNSStatus() DNSStatus {
	m.dnsMu.Lock()
	defer m.dnsMu.Unlock()
	return m.dnsStatus
}

// RunDNSSync reconciles the Cloudflare zone with the desired records and
// records the outcome. Only one run happens at a time; a run requested
// while another is active is queued once.
func (m *Manager) RunDNSSync(ctx context.Context) (DNSStatus, error) {
	st := m.Settings()
	if !dnsConfigured(st) {
		return m.DNSStatus(), ErrDNSNotConfigured
	}
	m.dnsMu.Lock()
	if m.dnsBusy {
		m.dnsPending = true
		m.dnsMu.Unlock()
		return m.DNSStatus(), ErrDNSBusy
	}
	m.dnsBusy = true
	m.dnsMu.Unlock()

	status := m.syncDNSOnce(ctx, st)

	m.dnsMu.Lock()
	m.dnsStatus = status
	m.dnsBusy = false
	rerun := m.dnsPending
	m.dnsPending = false
	m.dnsMu.Unlock()
	if rerun {
		m.TriggerDNSSync("queued change")
	}
	if !status.OK {
		return status, errors.New(status.Error)
	}
	return status, nil
}

func (m *Manager) syncDNSOnce(ctx context.Context, st PanelSettings) DNSStatus {
	now := time.Now().UnixMilli()
	target := st.DNSTarget
	if target == "" {
		ip, err := dns.PublicIP(ctx)
		if err != nil {
			return DNSStatus{LastSync: now, Error: "detect public IP: " + err.Error()}
		}
		target = ip
	}
	records := m.desiredDNSRecords(st.Domain, target)
	cf := &dns.Cloudflare{Token: st.DNSToken}
	res, err := cf.Sync(ctx, st.Domain, records)
	if err != nil {
		return DNSStatus{LastSync: now, Error: err.Error()}
	}
	log.Printf("dns: %d records for *.%s -> %s (%d created, %d updated, %d deleted)",
		len(records), st.Domain, target, res.Created, res.Updated, res.Deleted)
	return DNSStatus{LastSync: now, OK: true, Records: len(records), Target: target, Warnings: res.Warnings}
}

// TriggerDNSSync starts a background reconciliation when DNS management is
// configured. Safe to call from any context, including under m.mu: the
// configuration check happens inside the goroutine.
func (m *Manager) TriggerDNSSync(reason string) {
	go func() {
		if !dnsConfigured(m.Settings()) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, err := m.RunDNSSync(ctx); err != nil && !errors.Is(err, ErrDNSBusy) {
			log.Printf("dns sync (%s): %v", reason, err)
		}
	}()
}

// cleanupDNS removes all panel-managed records below the given (old)
// configuration after the feature was switched off.
func (m *Manager) cleanupDNS(st PanelSettings) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cf := &dns.Cloudflare{Token: st.DNSToken}
	res, err := cf.Sync(ctx, st.Domain, nil)
	if err != nil {
		log.Printf("dns cleanup for %s: %v", st.Domain, err)
		return
	}
	log.Printf("dns cleanup for %s: %d records removed", st.Domain, res.Deleted)
}

// dnsMaintenance runs from the scheduler every few minutes: it repairs
// drift hourly, retries failed syncs and — when no fixed target is pinned —
// follows the host's public IP like a DynDNS client.
func (m *Manager) dnsMaintenance() {
	st := m.Settings()
	if !dnsConfigured(st) {
		return
	}
	m.dnsMu.Lock()
	status := m.dnsStatus
	busy := m.dnsBusy
	m.dnsMu.Unlock()
	if busy {
		return
	}
	age := time.Since(time.UnixMilli(status.LastSync))
	switch {
	case status.LastSync == 0, age > time.Hour, !status.OK && age > 5*time.Minute:
		m.TriggerDNSSync("periodic")
	case st.DNSTarget == "" && status.Target != "":
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		ip, err := dns.PublicIP(ctx)
		cancel()
		if err == nil && ip != status.Target {
			log.Printf("dns: public IP changed %s -> %s", status.Target, ip)
			m.TriggerDNSSync("public IP changed")
		}
	}
}
