// Pani Networks proprietary license.
// (c) Copyright 2017 Pani Networks. All Rights Reserved

package main

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/osrg/gobgp/client"
	"github.com/osrg/gobgp/packet/bgp"
	"github.com/osrg/gobgp/table"
	"github.com/pkg/errors"
	log "github.com/romana/rlog"
	"google.golang.org/grpc"
)

func defaultGRPCOptions() []grpc.DialOption {
	return []grpc.DialOption{grpc.WithTimeout(time.Second * 2), grpc.WithBlock(), grpc.WithInsecure()}
}

func bgpAddRoute(client *client.Client, mask uint8, addr, nexthop string) ([]byte, error) {
	bgpAddrPrefix := bgp.NewIPAddrPrefix(mask, addr)

	pattr := make([]bgp.PathAttributeInterface, 0)
	pattr = append(pattr, bgp.NewPathAttributeNextHop(nexthop))
	pattr = append(pattr, bgp.NewPathAttributeOrigin(bgp.BGP_ORIGIN_ATTR_TYPE_INCOMPLETE))

	path := table.NewPath(nil, bgpAddrPrefix, false, pattr, time.Now(), false)

	return client.AddPath([]*table.Path{path})
}

func bgpDelRoute(client *client.Client, mask uint8, addr string) error {
	bgpAddrPrefix := bgp.NewIPAddrPrefix(mask, addr)
	pattr := make([]bgp.PathAttributeInterface, 0)
	pattr = append(pattr, bgp.NewPathAttributeOrigin(bgp.BGP_ORIGIN_ATTR_TYPE_INCOMPLETE))

	path := table.NewPath(nil, bgpAddrPrefix, false, pattr, time.Now(), false)

	return client.DeletePath([]*table.Path{path})
}

// goBgpWorker maintains goBgp connection and initiates synchronization.
func goBgpWorker(ctx context.Context, target string, in chan topologyMap, ticker *time.Ticker) {
	var err error
	var goBgpClient *client.Client
	var lastTopology topologyMap

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case lastTopology = <-in:
			case <-ticker.C:
				if err != nil || goBgpClient == nil {
					goBgpClient, err = client.NewWith(ctx,
						target,
						defaultGRPCOptions()...)
					if err != nil {
						log.Errorf("failed to connect to gobgp api server, %s", err)
						continue
					}
				}

				err = syncGoBgp(goBgpClient, lastTopology)
			}
		}
	}()
}

// syncGoBgp synchronizes goBgp RIB with topologyMap.
func syncGoBgp(client *client.Client, topology topologyMap) error {
	var err error

	// read RIB from goBgp
	resultTable, err := client.GetRIB(bgp.RF_IPv4_UC, nil)
	if err != nil {
		return errors.Wrap(err, "failed to read RIB data from gogbp api")
	}

	// parse RIB into destinations
	base := getDestinationsFromTable(resultTable)

	// parse topologyMap into destinations
	diff := getDestinationsFromTopology(topology)

	log.Debugf("Merging diff \n%s\n into base \n%s\n",
		diff, base)

	add, remove := mergeDestinations(base, diff)

	log.Debugf("Adding \n%s\nremoving \n%s\n", add, remove)

	for _, dest := range add {
		output, err2 := bgpAddRoute(client, dest.mask, dest.prefix.String(), dest.nexthop.String())
		if err != nil {
			err = errors.Wrapf(err, "failed to add route %s, output=%s, error=%s", dest, output, err2)
		}
	}

	for _, dest := range remove {
		err2 := bgpDelRoute(client, dest.mask, dest.prefix.String())
		if err != nil {
			err = errors.Wrapf(err, "failed to remove route %s, error=%s", dest, err2)
		}
	}
	return err
}

type destinations []destination

func (d destinations) String() string {
	var r string
	for _, dest := range d {
		if r == "" {
			r = dest.String()
			continue
		}

		r = fmt.Sprintf("%s\n%s", r, dest)
	}
	return r
}

func (d destinations) Contains(target destination) bool {
	for _, dest := range d {
		if dest.String() == target.String() {
			return true
		}
	}
	return false
}

func (a destinations) Len() int           { return len(a) }
func (a destinations) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a destinations) Less(i, j int) bool { return a[i].String() < a[j].String() }

// mergeDestinations produces 2 lists that have to be added removed/from base to
// make is similar to diff.
func mergeDestinations(base, diff destinations) (add, remove destinations) {
	for _, dest := range diff {
		if !base.Contains(dest) {
			add = append(add, dest)
		}
	}

	for _, dest := range base {
		if !diff.Contains(dest) {
			remove = append(remove, dest)
		}
	}

	return add, remove
}

// destination is a representation of an IP route.
type destination struct {
	prefix  net.IP
	mask    uint8
	nexthop net.IP
}

func (d destination) String() string {
	return fmt.Sprintf("%s/%d -> %s", d.prefix, d.mask, d.nexthop)
}

func parsePrefix(prefix string) (net.IP, uint8, error) {
	ip, ipnet, err := net.ParseCIDR(prefix)
	if err != nil {
		return nil, 0, err
	}
	size, _ := ipnet.Mask.Size()

	return ip, uint8(size), nil
}

// getDestinationsFromTopology converts topologyMap into a list of destinations.
func getDestinationsFromTopology(topology topologyMap) destinations {
	var result destinations
	for _, network := range topology {
		for prefixGroup, nexthopList := range network {
			prefix, mask, err := parsePrefix(prefixGroup)
			if err != nil {
				log.Errorf("failed to parse romana prefix %s", prefix)
				continue
			}

			for _, nh := range nexthopList {
				nexthop := net.ParseIP(nh)
				if nexthop == nil {
					log.Errorf("failed to parse nexthop %s", nh)
					continue
				}

				result = append(result, destination{prefix, mask, nexthop})
			}
		}
	}
	sort.Sort(result)

	return result
}

// getDestinationsFromTable converts goBgp RIB table into the list of destinations.
func getDestinationsFromTable(table *table.Table) destinations {
	var result destinations

	for k, v := range table.GetDestinations() {
		prefix, mask, err := parsePrefix(k)
		if err != nil {
			log.Errorf("failed to parse prefix %s, skipped", k)
			continue
		}

		paths := v.GetAllKnownPathList()

		if len(paths) != 1 {
			log.Errorf("expecting one path per destination, got %s %v", v, paths)
			continue
		}

		result = append(result, destination{prefix, mask, paths[0].GetNexthop()})
	}
	sort.Sort(result)

	return result
}
