package mdns

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"

	"github.com/celebdor/zeroconf"
	"github.com/miekg/dns"
	"golang.org/x/net/context"
)

var log = clog.NewWithPlugin("mdns")

type MDNS struct {
	Next      plugin.Handler
	Domain    string
	filter    string
	bindIface string
	mutex     *sync.RWMutex
	mdnsHosts *map[string]*zeroconf.ServiceEntry
}

func (m MDNS) ReplaceDomain(input string) string {
	// Replace input domain with our configured custom domain
	fqDomain := "." + strings.TrimSuffix(m.Domain, ".") + "."
	domainParts := strings.SplitN(input, ".", 2)
	// +1 so we strip the leading . as well
	suffixLen := len(domainParts[1]) + 1
	return input[0:len(input)-suffixLen] + fqDomain
}

func (m MDNS) AddARecord(msg *dns.Msg, state *request.Request, hosts map[string]*zeroconf.ServiceEntry, name string) bool {
	// Add A and AAAA record for name (if it exists) to msg.
	// A records need to be returned in both A and CNAME queries, this function
	// provides common code for doing so.
	answerEntry, present := hosts[name]
	if present {
		if answerEntry.AddrIPv4 != nil && state.QType() == dns.TypeA {
			aheader := dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}
			// TODO: Support multiple addresses
			msg.Answer = append(msg.Answer, &dns.A{Hdr: aheader, A: answerEntry.AddrIPv4[0]})
		}
		if answerEntry.AddrIPv6 != nil && state.QType() == dns.TypeAAAA {
			aaaaheader := dns.RR_Header{Name: name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60}
			msg.Answer = append(msg.Answer, &dns.AAAA{Hdr: aaaaheader, AAAA: answerEntry.AddrIPv6[0]})
		}
		return true
	}
	return false
}

func (m MDNS) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {

	log.Debug("Received query")
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true
	msg.RecursionAvailable = true
	state := request.Request{W: w, Req: r}
	log.Debugf("Looking for name: %s", state.QName())
	// Just for convenience so we don't have to keep dereferencing these
	mdnsHosts := *m.mdnsHosts

	if !strings.HasSuffix(state.QName(), m.Domain+".") {
		log.Debugf("Skipping due to query '%s' not in our domain '%s'", state.QName(), m.Domain)
		return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
	}

	if state.QType() != dns.TypeA && state.QType() != dns.TypeAAAA && state.QType() != dns.TypeSRV && state.QType() != dns.TypeCNAME {
		log.Debugf("Skipping due to unrecognized query type %v", state.QType())
		//return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
		return dns.RcodeServerFailure, nil
	}

	msg.Answer = []dns.RR{}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if m.AddARecord(msg, &state, mdnsHosts, state.Name()) {
		log.Debug(msg)
		w.WriteMsg(msg)
		return dns.RcodeSuccess, nil
	}

	log.Debugf("No records found for '%s', forwarding to next plugin.", state.QName())
	return dns.RcodeServerFailure, nil
}

func (m *MDNS) BrowseMDNS() {
	entriesCh := make(chan *zeroconf.ServiceEntry)
	mdnsHosts := make(map[string]*zeroconf.ServiceEntry)
	go func(results <-chan *zeroconf.ServiceEntry) {
		log.Debug("Retrieving mDNS entries")
		for entry := range results {
			// Make a copy of the entry so zeroconf can't later overwrite our changes
			localEntry := *entry
			log.Debugf("A Instance: %s, HostName: %s, AddrIPv4: %s, AddrIPv6: %s\n", localEntry.Instance, localEntry.HostName, localEntry.AddrIPv4, localEntry.AddrIPv6)
			if strings.Contains(localEntry.Instance, m.filter) {
				// Hacky - coerce .local to our domain
				// I was having trouble using domains other than .local. Need further investigation.
				// After further investigation, maybe this is working as intended:
				// https://lists.freedesktop.org/archives/avahi/2006-February/000517.html
				hostCustomDomain := m.ReplaceDomain(localEntry.HostName)
				mdnsHosts[hostCustomDomain] = entry
			} else {
				log.Debugf("Ignoring entry '%s' because it doesn't match filter '%s'\n",
					localEntry.Instance, m.filter)
			}
		}
	}(entriesCh)

	var iface net.Interface
	foundIface, err := GetBindInterface(m.bindIface)
	if err != nil {
		log.Errorf("Failed to find interface for '%s'\n", m.bindIface)
	} else {
		log.Debugf("Bind to interface '%s'\n", foundIface.Name)
		iface = *foundIface
	}
	_ = queryService("_workstation._tcp", entriesCh, iface, ZeroconfImpl{})

	m.mutex.Lock()
	defer m.mutex.Unlock()
	// Clear maps so we don't have stale entries
	for k := range *m.mdnsHosts {
		address, isExist := mdnsHosts[k]
		if !isExist {
			delete(*m.mdnsHosts, k)
			log.Infof("mdnsHosts: Delete %s", k)
		} else {
			(*m.mdnsHosts)[k] = address
		}
		delete(mdnsHosts, k)
	}

	// Copy values into the shared maps only after we've collected all of them.
	// This prevents us from having to lock during the entire mdns browse time.
	for k, address := range mdnsHosts {
		log.Infof("mdnsHosts: Add %s", k)
		(*m.mdnsHosts)[k] = address
	}
}

func queryService(service string, channel chan *zeroconf.ServiceEntry, iface net.Interface, z ZeroconfInterface) error {
	var opts zeroconf.ClientOption
	if iface.Name != "" {
		opts = zeroconf.SelectIfaces([]net.Interface{iface})
	}

	resolver, err := z.NewResolver(opts)
	if err != nil {
		log.Errorf("Failed to initialize %s resolver: %s", service, err.Error())
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// 	github.com/celebdor/zeroconf 支持 periodicQuery 函数持久发送 mDNS 请求。
	//	这里将超时时间设置为 1s, 实际上 periodicQuery 永远不会运行，每次运行 Browse 只会发送一个 mDNS 包
	err = resolver.Browse(ctx, service, "local.", channel)
	if err != nil {
		log.Errorf("Failed to browse %s records: %s", service, err.Error())
		return err
	}
	<-ctx.Done()
	return nil
}

func (m MDNS) Name() string { return "mdns" }

type ResponsePrinter struct {
	dns.ResponseWriter
}

func (r *ResponsePrinter) WriteMsg(res *dns.Msg) error {
	fmt.Fprintln(out, m)
	return r.ResponseWriter.WriteMsg(res)
}

var out io.Writer = os.Stdout

type ZeroconfInterface interface {
	NewResolver(...zeroconf.ClientOption) (ResolverInterface, error)
}

type ZeroconfImpl struct{}

func (z ZeroconfImpl) NewResolver(opts ...zeroconf.ClientOption) (ResolverInterface, error) {
	return zeroconf.NewResolver(opts...)
}

type ResolverInterface interface {
	Browse(context.Context, string, string, chan<- *zeroconf.ServiceEntry) error
}

const m = "mdns"

func GetBindInterface(ifacename string) (*net.Interface, error) {

	if ifacename != "" {
		return net.InterfaceByName(ifacename)
	}

	routeFile, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, err
	}
	defer routeFile.Close()

	defaultInterface := ""
	scanner := bufio.NewScanner(routeFile)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "00000000" {
			defaultInterface = fields[0]
			break
		}
	}
	if defaultInterface == "" {
		return nil, fmt.Errorf("default route interface is not found")
	}
	return net.InterfaceByName(defaultInterface)
}