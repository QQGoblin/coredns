package mdns

import (
	"context"
	"fmt"
	"github.com/celebdor/zeroconf"
	"net"
	"os"
)

const (
	defaultPublicNamePrefix = "host-mgr-"
	defaultServiceName      = "_workstation._tcp"
	defaultServicePort      = 12346
)

func defaultPublicName() string {
	hostname, _ := os.Hostname()
	if len(hostname) < 12 {
		return fmt.Sprintf("%s%s", defaultPublicNamePrefix, hostname)
	}
	return fmt.Sprintf("%s%s.local", defaultPublicNamePrefix, hostname[len(hostname)-12:])
}

func publicHostname(ctx context.Context, instancePrefix, hostname, domain string, bindInface string) error {

	iface, err := net.InterfaceByName(bindInface)
	if err != nil {
		return err
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return err
	}

	if len(addrs) == 0 {
		return fmt.Errorf("%s is not address found", iface.Name)
	}

	publicAddress := addrs[0].(*net.IPNet).IP.String()

	instanceName := fmt.Sprintf("%s-hostname-public", instancePrefix)

	server, err := zeroconf.RegisterProxy(instanceName, defaultServiceName, domain, defaultServicePort,
		hostname, []string{publicAddress}, []string{"txtv=0", "lo=1", "la=2"}, []net.Interface{*iface})

	defer server.Shutdown()

	select {
	case <-ctx.Done():
	}

	return nil
}
