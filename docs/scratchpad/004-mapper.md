# Record Mapper

- Implemented `internal/mapper/mapper.go` which converts raw ATProto JSON into SSB message structures (`map[string]interface{}`)
- Handled records:
  - `app.bsky.feed.post` -> `post` (with reply mapping and mention mapping)
  - `app.bsky.feed.like` -> `vote`
  - `app.bsky.feed.repost` -> `post` (with text containing the reposted ref)
  - `app.bsky.graph.follow` -> `contact` (following: true)
  - `app.bsky.graph.block` -> `contact` (blocking: true)
  - `app.bsky.actor.profile` -> `about`
- Added `ReplaceATProtoRefs` which takes a lookup callback to swap ATProto URIs/DIDs with SSB refs before signing and publishing.
- Wrote and verified unit tests (`mapper_test.go`).