# 018 README Section: Overview

## Project Header

**Mr Valinsky's Adequate Bridge** — An ATProto-to-SSB bridge with an embedded Room2 server.

## What It Does

- Ingests ATProto repository events from the Bluesky firehose (`subscribeRepos`)
- Maps supported record types (posts, likes, reposts, follows, blocks, profiles) to SSB message formats
- Deterministically derives SSB bot identities from ATProto DIDs using a master seed
- Publishes translated messages to SSB feeds through an embedded local sbot
- Runs an integrated SSB Room2 server for peer discovery and message distribution
- Mirrors ATProto blobs (images, media) into the SSB blob store with CID tracking
- Provides an admin web UI for monitoring, triage, and operational management

## Supported Record Types

| ATProto Collection | SSB Message Type | Description |
|----|----|----|
| `app.bsky.feed.post` | `post` | Text posts with mentions and links |
| `app.bsky.feed.like` | `like` | Likes referencing a subject record |
| `app.bsky.feed.repost` | `repost` | Reposts referencing a subject record |
| `app.bsky.graph.follow` | `contact` | Follow relationships |
| `app.bsky.graph.block` | `block_v2` | Block relationships |
| `app.bsky.actor.profile` | `about` | Profile name, description, avatar |
