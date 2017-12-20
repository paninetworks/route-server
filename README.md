# Route server

Route server is a configuration manager for the BIRD routing software
that generates dynamic config and restarts bird when Romana topology changes.

This allows the Route Server to maintain BGP peering with every Romana node automatically importing routes from all the nodes and exporting
to the router which doesn't support dynamic peering BGP.

Route server expects a template to be provided - it will render teamplate into a BIRD config file injecting Romana specific information.


Example template:
```
{{ $id := index .Args "RouterID" -}}
router id  {{ $id }};

{{ $networks := index .Args "Topology" -}}
{{ range $net, $topology := $networks -}}

	{{ range $cidr, $hosts := $topology -}}
		{{ range $host := $hosts }}

protocol bgp {
	# importing {{ $cidr }} from {{ $host }}
	import all;

	local as 65534;
	neighbor {{ $host }} as 65534;
}

		{{ end }}
	{{ end }}
{{ end }}
```

Romana related information is available via `.Args` variable using `.Topology` key.
Example above will be rendered into the bird config like

```
router id  ip-192-168-99-10;

protocol bgp {
	# importing 10.112.0.0/15 from 192.168.99.10
	import all;

	local as 65534;
	neighbor 192.168.99.10 as 65534;
}
```

with `protocol bgp` section for every Romana node.


# gobgp support

gobgpd server is supported instead of bird to allow more flexible conviguration.

gobgp is configured via 2 sources, via config file and via API, some configurations can only be done via API.

Example config for gobgpd server:
```
[global.config]
  as = 65000
  router-id = "10.0.0.1"

[[neighbors]]
[neighbors.config]
  neighbor-address = "192.168.1.1"
  peer-as = 65000

[[neighbors]]
[neighbors.config]
  neighbor-address = "192.168.1.9"
  peer-as = 65000
```

Gobgpd config can be rendered and managed exactly same way bird config does, smae `kill -HUP <pid>` strategy works to force running server to update it's configuration.

However, gobgp doesn't allow static routes in it's config file, instead, routes must be published via API once gobgpd is started.

Example publishing route:
```
# gobgp global rib add 10.0.0.0/24 nexthop 192.168.1.5
# gobgp global rib
   Network              Next Hop             AS_PATH              Age        Attrs
*> 10.0.0.0/24          192.168.1.5                               00:00:08   [{Origin: ?}]
```

Code in this repo can export static routes into the running gobgpd instance in form of `prefix-group-cidr -> nexthop` where nexthop is a first server in that group.

In order to enable this feature server needs `-gobgp-target` parameter pointing to gobgpd API. When parameter not provided, feature is not enabled.

Default API port of gobgpd is `50051` which makes most useful way of specifiying the flag:
```
# sudo ./route-server -gobgp-target ":50051"
```

Properly working gobgpd export will create routes like:
```
# gobgp global rib
   Network              Next Hop             AS_PATH              Age        Attrs
*> 10.112.0.0/15        192.168.99.10                             00:00:25   [{Origin: ?}]
*> 10.114.0.0/15        192.168.99.11                             00:00:25   [{Origin: ?}]
```

where Next hop points to the first server in prefix group, and `Origin: ?`.
