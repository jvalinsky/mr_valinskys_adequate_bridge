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
		"httpAuth.requestSolution",
		"httpAuth.invalidateAllSolutions",
		"invite.create",
		"invite.use",
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
		"tunnel.announce",
		"tunnel.leave",
		"tunnel.ping",
	},
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
