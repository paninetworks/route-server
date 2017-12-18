package main

import (
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/osrg/gobgp/client"
	"github.com/osrg/gobgp/packet/bgp"
	"github.com/osrg/gobgp/table"
	"google.golang.org/grpc"
)

/*
(*table.Table)(0xc42013c660)({
 routeFamily: (bgp.RouteFamily) ipv4-unicast,
 destinations: (map[string]*table.Destination) (len=1) {
  (string) (len=11) "10.0.0.0/24": (*table.Destination)(0xc42015e240)(Destination NLRI: 10.0.0.0/24)
 }
})
*/

/*
([]*table.Path) (len=1 cap=1) {
 (*table.Path)(0xc4201b8120)({ 10.0.0.0/24 | src: <nil>, nh: 192.168.1.5 })
}
*/
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

/*
func main() {
	client, err := client.NewWith(context.Background(), "", defaultGRPCOptions()...)
	if err != nil {
		panic(err)
	}

	resultTable, err := client.GetRIB(bgp.RF_IPv4_UC, nil)
	if err != nil {
		panic(err)
	}

	spew.Dump(resultTable)

	output, err := bgpAddRoute(client, 24, "10.0.1.0", "192.168.1.5")
	if err != nil {
		panic(err)
	}

	spew.Dump(output)

	time.Sleep(30 * time.Second)

	err = bgpDelRoute(client, 24,  "10.0.1.0")
	if err != nil {
		panic(err)
	}
}
*/

func syncGoBgp(client *client.Client, topology topologyMap) error {
	resultTable, err := client.GetRIB(bgp.RF_IPv4_UC, nil)
	if err != nil {
		panic(err)
	}

	base := getDestinationsFromTable(resultTable)
	diff := getDestinationsFromTopology(topology)
	fmt.Printf("Merging diff \n%s\n into base \n%s\n",
		diff, base)

	add, remove := mergeDestinations(base, diff)
	fmt.Printf("Adding \n%s\nremoving \n%s\n", add, remove)

	for _, dest := range add {
		output, err := bgpAddRoute(client, dest.mask, dest.prefix.String(), dest.nexthop.String())
		if err != nil {
			fmt.Printf("ERROR: failed to add route %s to bgp, output=%s, error=%s", dest, output, err)
			continue
		}
	}

	for _, dest := range remove {
		err := bgpDelRoute(client, dest.mask, dest.prefix.String())
		if err != nil {
			fmt.Printf("ERROR: failed to add route %s to bgp, error=%s", dest, err)
			continue
		}
	}
	return nil
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

func getDestinationsFromTopology(topology topologyMap) destinations {
	var result destinations
	for _, network := range topology {
		for prefixGroup, nexthopList := range network {
			prefix, mask, err := parsePrefix(prefixGroup)
			if err != nil {
				fmt.Printf("ERROR: failed to parse romana prefix group %s\n", prefix)
				continue
			}

			for _, nh := range nexthopList {
				nexthop := net.ParseIP(nh)
				if nexthop == nil {
					fmt.Printf("ERROR: failed to parse nexthop %s\n", nh)
					continue
				}

				result = append(result, destination{prefix, mask, nexthop})
			}
		}
	}
	sort.Sort(result)

	return result
}

func getDestinationsFromTable(table *table.Table) destinations {
	var result destinations

	for k, v := range table.GetDestinations() {
		prefix, mask, err := parsePrefix(k)
		if err != nil {
			fmt.Printf("ERROR: failed to parse prefix %s\n", k)
			continue
		}

		paths := v.GetAllKnownPathList()

		if len(paths) != 1 {
			fmt.Printf("ERROR: expect one path per destination, got %s %v\n", v, paths)
			continue
		}
		// fmt.Printf("%s %s", v, paths[0].GetNexthop())

		result = append(result, destination{prefix, mask, paths[0].GetNexthop()})
	}
	sort.Sort(result)

	return result
}
