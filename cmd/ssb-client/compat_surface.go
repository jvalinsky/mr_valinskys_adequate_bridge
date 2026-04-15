package main

import "sort"

var requiredManifestByType = map[string][]string{
	"async": {
		"whoami",
		"gossip.ping",
		"blobs.has",
		"blobs.size",
		"blobs.want",
		"room.metadata",
		"room.listAliases",
		"room.registerAlias",
		"room.revokeAlias",
		"tunnel.isRoom",
	},
	"source": {
		"createHistoryStream",
		"replicate.upto",
		"blobs.get",
		"blobs.createWants",
		"room.members",
		"room.attendants",
		"tunnel.endpoints",
	},
	"sink": {
		"blobs.add",
	},
	"duplex": {
		"ebt.replicate",
		"tunnel.connect",
	},
	"sync": {
		"manifest",
		"tunnel.announce",
		"tunnel.leave",
		"tunnel.ping",
	},
}

var explicitlyUnsupportedRPCMethods = map[string]string{
	"invite.create":                   "not registered by cmd/ssb-client sbot; room HTTP invite creation is exposed through the room runtime",
	"httpAuth.requestSolution":        "not registered by cmd/ssb-client sbot; available on the room runtime muxrpc surface",
	"httpAuth.invalidateAllSolutions": "not registered by cmd/ssb-client sbot; available on the room runtime muxrpc surface",
	"metafeeds":                       "out of Room+Replication scope and not advertised",
	"indexFeeds":                      "out of Room+Replication scope and not advertised",
	"bipfHistory":                     "out of Room+Replication scope and not advertised",
}

func flattenManifestByType(byType map[string][]string) []string {
	all := make([]string, 0)
	for _, names := range byType {
		all = append(all, names...)
	}
	sort.Strings(all)
	return all
}

func missingMethods(required, actual []string) []string {
	set := make(map[string]struct{}, len(actual))
	for _, m := range actual {
		set[m] = struct{}{}
	}
	missing := make([]string, 0)
	for _, m := range required {
		if _, ok := set[m]; !ok {
			missing = append(missing, m)
		}
	}
	sort.Strings(missing)
	return missing
}
