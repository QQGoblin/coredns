package hook

import (
	clog "github.com/coredns/coredns/plugin/pkg/log"
)

var log = clog.NewWithPlugin("hook")

type hook struct {
	resolveConfig string
	nameservers   []string
}

func (h *hook) OnStartup() error {

	if len(h.nameservers) == 0 {
		return nil
	}
	return override(h.resolveConfig, h.nameservers)
}

func (h *hook) OnReload() error {
	return nil
}

func (h *hook) OnFinalShutdown() error {

	if len(h.nameservers) == 0 {
		return nil
	}
	return override(h.resolveConfig, nil)
}

func override(resolveConfig string, injectServers []string) error {

	dnsConfig, err := NewDNSConfigFromFile(resolveConfig)
	if err != nil {
		return err
	}
	if len(injectServers) != 0 {
		dnsConfig.InjectServers = injectServers
	}

	if err = dnsConfig.Writer(resolveConfig); err != nil {
		log.Errorf("Could not write dns nameserver in the file", "path", resolveConfig)
	}
	return nil
}
