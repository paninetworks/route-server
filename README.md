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
