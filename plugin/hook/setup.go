package hook

import (
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/plugin"
)

func init() { plugin.Register("hook", setup) }

func setup(c *caddy.Controller) error {
	nameservers, resolveConfig := parse(c)

	h := &hook{nameservers: nameservers, resolveConfig: resolveConfig}

	c.OnStartup(h.OnStartup)
	c.OnRestart(h.OnReload)
	c.OnFinalShutdown(h.OnFinalShutdown)
	c.OnRestartFailed(h.OnStartup)

	// Don't do AddPlugin, as health is not *really* a plugin just a separate webserver running.
	return nil
}

const (
	defaultResolveConfig = "/etc/resolv.conf"
)

func parse(c *caddy.Controller) ([]string, string) {

	var nameservers []string
	resolvConfig := defaultResolveConfig
	for c.Next() {
		args := c.RemainingArgs()

		switch len(args) {
		case 0:
		case 1:
			resolvConfig = args[0]
		default:
			if args[0] != "" {
				resolvConfig = args[0]
			}
			nameservers = args[1:]
		}
	}
	return nameservers, resolvConfig
}
