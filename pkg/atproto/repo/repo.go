package repo

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	cbornode "github.com/ipfs/go-ipld-cbor"
	"github.com/ipld/go-car"
	mh "github.com/multiformats/go-multihash"
)

func init() {
	cbornode.RegisterCborType(SignedCommit{})
	cbornode.RegisterCborType(NodeData{})
	cbornode.RegisterCborType(TreeEntry{})
}

func computeMultihash(data []byte) mh.Multihash {
	h, _ := mh.Sum(data, mh.DBL_SHA2_256, -1)
	return h
}

type SignedCommit struct {
	Did     string   `json:"did" refmt:"did"`
	Version int64    `json:"version" refmt:"version"`
	Prev    *cid.Cid `json:"prev,omitempty" refmt:"prev,omitempty"`
	Data    cid.Cid  `json:"data" refmt:"data"`
	Sig     []byte   `json:"sig" refmt:"sig"`
	Rev     string   `json:"rev,omitempty" refmt:"rev,omitempty"`
}

type Repo struct {
	did    string
	commit SignedCommit
	bs     *memBlockstore
	root   *mstNode
}

type NodeData struct {
	Left    *cid.Cid    `json:"l,omitempty" refmt:"l,omitempty"`
	Entries []TreeEntry `json:"e" refmt:"e"`
}

type TreeEntry struct {
	PrefixLen int64    `json:"p" refmt:"p"`
	KeySuffix []byte   `json:"k" refmt:"k"`
	Val       cid.Cid  `json:"v" refmt:"v"`
	Tree      *cid.Cid `json:"t,omitempty" refmt:"t,omitempty"`
}

type mstEntry struct {
	Key   string
	Value cid.Cid
	Tree  *mstNode
}

type mstNode struct {
	bs     *memBlockstore
	cid    cid.Cid
	loaded bool
	Left   *mstNode
	List   []mstEntry
}

type memBlockstore struct {
	blocks map[string]blocks.Block
}

func ReadRepoFromCar(ctx context.Context, r io.Reader) (*Repo, error) {
	bs, rootCID, err := ingestCAR(r)
	if err != nil {
		return nil, err
	}

	store := cbornode.NewCborStore(bs)
	var commit SignedCommit
	if err := store.Get(ctx, rootCID, &commit); err != nil {
		return nil, fmt.Errorf("load repo commit: %w", err)
	}

	return &Repo{
		commit: commit,
		bs:     bs,
		root:   newMSTNode(bs, commit.Data),
	}, nil
}

func (r *Repo) RepoDid() string {
	return r.commit.Did
}

func (r *Repo) SignedCommit() SignedCommit {
	return r.commit
}

func (r *Repo) GetRecordBytes(ctx context.Context, path string) (cid.Cid, *[]byte, error) {
	recordCID, ok, err := findRecordCID(ctx, r.root, strings.TrimSpace(path))
	if err != nil {
		return cid.Undef, nil, err
	}
	if !ok {
		return cid.Undef, nil, fmt.Errorf("resolving rpath within mst: not found")
	}
	block, err := r.bs.Get(ctx, recordCID)
	if err != nil {
		return cid.Undef, nil, err
	}
	raw := block.RawData()
	return recordCID, &raw, nil
}

func (r *Repo) ForEach(ctx context.Context, prefix string, cb func(k string, v cid.Cid) error) error {
	return walkMST(ctx, r.root, strings.TrimSpace(prefix), cb)
}

func ingestCAR(r io.Reader) (*memBlockstore, cid.Cid, error) {
	reader, err := car.NewCarReader(r)
	if err != nil {
		return nil, cid.Undef, fmt.Errorf("open car reader: %w", err)
	}

	bs := &memBlockstore{blocks: map[string]blocks.Block{}}
	for {
		block, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, cid.Undef, fmt.Errorf("read car block: %w", err)
		}
		if err := bs.Put(context.Background(), block); err != nil {
			return nil, cid.Undef, fmt.Errorf("store car block: %w", err)
		}
	}
	return bs, reader.Header.Roots[0], nil
}

func newMSTNode(bs *memBlockstore, root cid.Cid) *mstNode {
	return &mstNode{
		bs:  bs,
		cid: root,
	}
}

func walkMST(ctx context.Context, node *mstNode, from string, cb func(k string, v cid.Cid) error) error {
	if node == nil {
		return nil
	}
	if err := node.ensureLoaded(ctx); err != nil {
		return err
	}
	if node.Left != nil {
		if err := walkMST(ctx, node.Left, from, cb); err != nil {
			return err
		}
	}
	for _, entry := range node.List {
		if entry.Key >= from {
			if err := cb(entry.Key, entry.Value); err != nil {
				return err
			}
		}
		if entry.Tree != nil {
			if err := walkMST(ctx, entry.Tree, from, cb); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func findRecordCID(ctx context.Context, node *mstNode, path string) (cid.Cid, bool, error) {
	if node == nil {
		return cid.Undef, false, nil
	}
	if err := node.ensureLoaded(ctx); err != nil {
		return cid.Undef, false, err
	}
	if node.Left != nil {
		if value, ok, err := findRecordCID(ctx, node.Left, path); err != nil || ok {
			return value, ok, err
		}
	}
	for _, entry := range node.List {
		if entry.Key == path {
			return entry.Value, true, nil
		}
		if entry.Tree != nil {
			if value, ok, err := findRecordCID(ctx, entry.Tree, path); err != nil || ok {
				return value, ok, err
			}
		}
	}
	return cid.Undef, false, nil
}

func (n *mstNode) ensureLoaded(ctx context.Context) error {
	if n == nil || n.loaded {
		return nil
	}
	store := cbornode.NewCborStore(n.bs)
	var data NodeData
	if err := store.Get(ctx, n.cid, &data); err != nil {
		return err
	}

	if data.Left != nil {
		n.Left = newMSTNode(n.bs, *data.Left)
	}

	lastKey := ""
	for _, entry := range data.Entries {
		prefixLen := int(entry.PrefixLen)
		if prefixLen > len(lastKey) {
			prefixLen = len(lastKey)
		}
		key := lastKey[:prefixLen] + string(entry.KeySuffix)
		item := mstEntry{
			Key:   key,
			Value: entry.Val,
		}
		if entry.Tree != nil {
			item.Tree = newMSTNode(n.bs, *entry.Tree)
		}
		n.List = append(n.List, item)
		lastKey = key
	}

	n.loaded = true
	return nil
}

func (m *memBlockstore) Get(_ context.Context, id cid.Cid) (blocks.Block, error) {
	block, ok := m.blocks[id.String()]
	if !ok {
		return nil, fmt.Errorf("block %s not found", id)
	}
	return block, nil
}

func (m *memBlockstore) Put(_ context.Context, block blocks.Block) error {
	m.blocks[block.Cid().String()] = block
	return nil
}

func (m *memBlockstore) Len() int {
	return len(m.blocks)
}

type Blockstore interface {
	Put(context.Context, blocks.Block) error
	Get(context.Context, cid.Cid) (blocks.Block, error)
}

type WriteRepo struct {
	did         string
	bs          *memBlockstore
	records     map[string]cid.Cid
	commitCache *SignedCommit
}

func NewRepo(did string, bs Blockstore) *WriteRepo {
	ms, ok := bs.(*memBlockstore)
	if !ok {
		ms = &memBlockstore{blocks: map[string]blocks.Block{}}
	}
	return &WriteRepo{
		did:     did,
		bs:      ms,
		records: make(map[string]cid.Cid),
	}
}

type SignFunc func(ctx context.Context, did string, data []byte) ([]byte, error)

func (r *WriteRepo) CreateRecord(ctx context.Context, collection string, record any) (cid.Cid, string, error) {
	data, err := cbornode.DumpObject(record)
	if err != nil {
		return cid.Undef, "", fmt.Errorf("marshal record: %w", err)
	}
	mh := computeMultihash(data)
	c := cid.NewCidV1(0x55, mh)
	blk, err := blocks.NewBlockWithCid(data, c)
	if err != nil {
		return cid.Undef, "", fmt.Errorf("create block: %w", err)
	}
	if err := r.bs.Put(ctx, blk); err != nil {
		return cid.Undef, "", fmt.Errorf("put record block: %w", err)
	}
	key := collection + "/" + r.generateRkey()
	r.records[key] = c
	return c, key, nil
}

func (r *WriteRepo) Commit(ctx context.Context, signFn SignFunc) (cid.Cid, string, error) {
	treeCID, err := r.buildMST(ctx)
	if err != nil {
		return cid.Undef, "", fmt.Errorf("build mst: %w", err)
	}
	commit := SignedCommit{
		Did:     r.did,
		Version: 3,
		Data:    treeCID,
	}
	if r.commitCache != nil {
		prevCid := r.commitCache.Data
		commit.Prev = &prevCid
	}
	commitData, err := cbornode.DumpObject(commit)
	if err != nil {
		return cid.Undef, "", fmt.Errorf("marshal commit: %w", err)
	}
	if signFn != nil {
		sig, err := signFn(ctx, r.did, commitData)
		if err != nil {
			return cid.Undef, "", fmt.Errorf("sign commit: %w", err)
		}
		commit.Sig = sig
	} else {
		commit.Sig = []byte("test-signature")
	}
	commitData, _ = cbornode.DumpObject(commit)
	mh := computeMultihash(commitData)
	commitCID := cid.NewCidV1(0x55, mh)
	r.commitCache = &commit
	return commitCID, commit.Rev, nil
}

func (r *WriteRepo) buildMST(ctx context.Context) (cid.Cid, error) {
	if len(r.records) == 0 {
		data, err := cbornode.DumpObject(NodeData{Entries: []TreeEntry{}})
		if err != nil {
			return cid.Undef, fmt.Errorf("marshal empty node: %w", err)
		}
		mh := computeMultihash(data)
		c := cid.NewCidV1(0x55, mh)
		blk, err := blocks.NewBlockWithCid(data, c)
		if err != nil {
			return cid.Undef, fmt.Errorf("create block: %w", err)
		}
		if err := r.bs.Put(ctx, blk); err != nil {
			return cid.Undef, fmt.Errorf("put empty node: %w", err)
		}
		return c, nil
	}
	entries := make([]TreeEntry, 0, len(r.records))
	for k, v := range r.records {
		entries = append(entries, TreeEntry{
			KeySuffix: []byte(k),
			Val:       v,
		})
	}
	data, err := cbornode.DumpObject(NodeData{Entries: entries})
	if err != nil {
		return cid.Undef, fmt.Errorf("marshal mst data: %w", err)
	}
	mh := computeMultihash(data)
	c := cid.NewCidV1(0x55, mh)
	blk, err := blocks.NewBlockWithCid(data, c)
	if err != nil {
		return cid.Undef, fmt.Errorf("create block: %w", err)
	}
	if err := r.bs.Put(ctx, blk); err != nil {
		return cid.Undef, fmt.Errorf("put mst node: %w", err)
	}
	return c, nil
}

func (r *WriteRepo) generateRkey() string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(len(r.records)+1))
	return fmt.Sprintf("%016x", buf[:])
}

func (r *WriteRepo) WriteCAR(w io.Writer) error {
	commitCID := cid.Cid{}
	roots := make([]cid.Cid, 0, 1)
	if r.commitCache != nil {
		data, err := cbornode.DumpObject(*r.commitCache)
		if err == nil {
			mh := computeMultihash(data)
			commitCID = cid.NewCidV1(0x55, mh)
			roots = append(roots, commitCID)
			blk, err := blocks.NewBlockWithCid(data, commitCID)
			if err == nil {
				r.bs.Put(context.Background(), blk)
			}
		}
	}
	if len(roots) == 0 {
		roots = append(roots, cid.Cid{})
	}
	header := car.CarHeader{Version: 1, Roots: roots}
	if err := car.WriteHeader(&header, w); err != nil {
		return fmt.Errorf("write car header: %w", err)
	}
	for _, blk := range r.bs.blocks {
		var total uint64
		cidBytes := blk.Cid().Bytes()
		rawData := blk.RawData()
		total = uint64(len(cidBytes) + len(rawData))
		var prefix [binary.MaxVarintLen64]byte
		prefixLen := binary.PutUvarint(prefix[:], total)
		if _, err := w.Write(prefix[:prefixLen]); err != nil {
			return fmt.Errorf("write prefix: %w", err)
		}
		if _, err := w.Write(cidBytes); err != nil {
			return fmt.Errorf("write cid: %w", err)
		}
		if _, err := w.Write(rawData); err != nil {
			return fmt.Errorf("write data: %w", err)
		}
	}
	return nil
}
