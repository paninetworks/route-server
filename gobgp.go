package main

import "github.com/osrg/gobgp/client"
import "github.com/osrg/gobgp/packet/bgp"
import "github.com/osrg/gobgp/table"
import "time"
import "google.golang.org/grpc"

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
	return []grpc.DialOption{grpc.WithTimeout(time.Second), grpc.WithBlock(), grpc.WithInsecure()}
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

func syncGoBgp(gobgpClient *client.Client, topology topologyMap) error {
	return nil
}
