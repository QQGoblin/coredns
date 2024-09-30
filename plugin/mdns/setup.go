package mdns

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"

	"github.com/celebdor/zeroconf"
	"github.com/coredns/caddy"
)

func init() {
	caddy.RegisterPlugin("mdns", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

func setup(c *caddy.Controller) error {
	c.Next()
	c.NextArg()
	domain := c.Val()
	publicName := defaultPublicName()
	// Note that a filter of "" will match everything
	filter := ""
	bindInface := ""
	if c.NextArg() {
		filter = c.Val()
	}
	if c.NextArg() {
		bindInface = c.Val()
	}
	if c.NextArg() {
		publicName = c.Val()
	}
	if c.NextArg() {
		return plugin.Error("mdns", c.ArgErr())
	}

	// Because the plugin interface uses a value receiver, we need to make these
	// pointers so all copies of the plugin point at the same maps.
	mdnsHosts := make(map[string]*zeroconf.ServiceEntry)
	mutex := sync.RWMutex{}

	m := MDNS{Domain: strings.TrimSuffix(domain, "."), filter: filter, bindIface: bindInface, mutex: &mutex, mdnsHosts: &mdnsHosts}

	c.OnStartup(func() error {
		go browseLoop(&m)

		go func() {
			if err := publicHostname(context.TODO(), filter, publicName, domain, bindInface); err != nil {
				log.Errorf("start public hostname failed: %v", err)
			}
		}()
		return nil
	})

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		m.Next = next
		return m
	})

	return nil
}

func browseLoop(m *MDNS) {
	for {
		m.BrowseMDNS()
		// 5 seconds seems to be the minimum ttl that the cache plugin will allow
		// Since each browse operation takes around 2 seconds, this should be fine
		time.Sleep(5 * time.Second)
	}
}
