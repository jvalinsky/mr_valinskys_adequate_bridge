package atproto

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	cbornode "github.com/ipfs/go-ipld-cbor"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/syntax"
)

type RepoStrongRef struct {
	Uri string `json:"uri" refmt:"uri"`
	Cid string `json:"cid" refmt:"cid"`
}

type RepoDefs_CommitMeta struct {
	Cid string `json:"cid,omitempty" refmt:"cid,omitempty"`
	Rev string `json:"rev,omitempty" refmt:"rev,omitempty"`
}

type ServerCreateSession_Input struct {
	AllowTakendown  *bool   `json:"allowTakendown,omitempty"`
	AuthFactorToken *string `json:"authFactorToken,omitempty"`
	Identifier      string  `json:"identifier"`
	Password        string  `json:"password"`
}

type ServerCreateSession_Output struct {
	AccessJwt       string  `json:"accessJwt"`
	Active          *bool   `json:"active,omitempty"`
	Did             string  `json:"did"`
	DidDoc          *any    `json:"didDoc,omitempty"`
	Email           *string `json:"email,omitempty"`
	EmailAuthFactor *bool   `json:"emailAuthFactor,omitempty"`
	EmailConfirmed  *bool   `json:"emailConfirmed,omitempty"`
	Handle          string  `json:"handle"`
	RefreshJwt      string  `json:"refreshJwt"`
	Status          *string `json:"status,omitempty"`
}

type ServerCreateAccount_Input struct {
	Did        *string `json:"did,omitempty"`
	Email      *string `json:"email,omitempty"`
	Handle     string  `json:"handle"`
	InviteCode *string `json:"inviteCode,omitempty"`
	Password   *string `json:"password,omitempty"`
}

type ServerCreateAccount_Output struct {
	AccessJwt  string `json:"accessJwt"`
	Did        string `json:"did"`
	Handle     string `json:"handle"`
	RefreshJwt string `json:"refreshJwt"`
}

type RepoCreateRecord_Input struct {
	Collection string                      `json:"collection"`
	Record     *lexutil.LexiconTypeDecoder `json:"record"`
	Repo       string                      `json:"repo"`
	Rkey       *string                     `json:"rkey,omitempty"`
	SwapCommit *string                     `json:"swapCommit,omitempty"`
	Validate   *bool                       `json:"validate,omitempty"`
}

type RepoCreateRecord_Output struct {
	Cid              string               `json:"cid"`
	Commit           *RepoDefs_CommitMeta `json:"commit,omitempty"`
	Uri              string               `json:"uri"`
	ValidationStatus *string              `json:"validationStatus,omitempty"`
}

type RepoDeleteRecord_Input struct {
	Collection string  `json:"collection"`
	Repo       string  `json:"repo"`
	Rkey       string  `json:"rkey"`
	SwapCommit *string `json:"swapCommit,omitempty"`
	SwapRecord *string `json:"swapRecord,omitempty"`
}

type RepoDeleteRecord_Output struct {
	Commit *RepoDefs_CommitMeta `json:"commit,omitempty"`
}

type RepoGetRecord_Output struct {
	Cid   *string                     `json:"cid,omitempty"`
	Uri   string                      `json:"uri"`
	Value *lexutil.LexiconTypeDecoder `json:"value"`
}

type RepoUploadBlob_Output struct {
	Blob *lexutil.LexBlob `json:"blob"`
}

type SyncSubscribeRepos_Commit struct {
	Blobs    []lexutil.LexLink            `json:"blobs" refmt:"blobs"`
	Blocks   lexutil.LexBytes             `json:"blocks,omitempty" refmt:"blocks,omitempty"`
	Commit   lexutil.LexLink              `json:"commit" refmt:"commit"`
	Ops      []*SyncSubscribeRepos_RepoOp `json:"ops" refmt:"ops"`
	PrevData *lexutil.LexLink             `json:"prevData,omitempty" refmt:"prevData,omitempty"`
	Rebase   bool                         `json:"rebase" refmt:"rebase"`
	Repo     string                       `json:"repo" refmt:"repo"`
	Rev      string                       `json:"rev" refmt:"rev"`
	Seq      int64                        `json:"seq" refmt:"seq"`
	Since    *string                      `json:"since,omitempty" refmt:"since,omitempty"`
	Time     string                       `json:"time" refmt:"time"`
	TooBig   bool                         `json:"tooBig" refmt:"tooBig"`
}

type SyncSubscribeRepos_RepoOp struct {
	Action string           `json:"action" refmt:"action"`
	Cid    *lexutil.LexLink `json:"cid" refmt:"cid"`
	Path   string           `json:"path" refmt:"path"`
	Prev   *lexutil.LexLink `json:"prev,omitempty" refmt:"prev,omitempty"`
}

type SyncSubscribeRepos_Identity struct {
	Did    string  `json:"did" refmt:"did"`
	Handle *string `json:"handle,omitempty" refmt:"handle,omitempty"`
	Seq    int64   `json:"seq" refmt:"seq"`
	Time   string  `json:"time" refmt:"time"`
}

type SyncSubscribeRepos_Account struct {
	Active bool    `json:"active" refmt:"active"`
	Did    string  `json:"did" refmt:"did"`
	Seq    int64   `json:"seq" refmt:"seq"`
	Status *string `json:"status,omitempty" refmt:"status,omitempty"`
	Time   string  `json:"time" refmt:"time"`
}

type SyncSubscribeRepos_Info struct {
	Message *string `json:"message,omitempty" refmt:"message,omitempty"`
	Name    string  `json:"name" refmt:"name"`
}

type SyncSubscribeRepos_Sync struct {
	Blocks lexutil.LexBytes `json:"blocks,omitempty" refmt:"blocks,omitempty"`
	Did    string           `json:"did" refmt:"did"`
	Rev    string           `json:"rev" refmt:"rev"`
	Seq    int64            `json:"seq" refmt:"seq"`
	Time   string           `json:"time" refmt:"time"`
}

func init() {
	cbornode.RegisterCborType(SyncSubscribeRepos_Commit{})
	cbornode.RegisterCborType(SyncSubscribeRepos_RepoOp{})
	cbornode.RegisterCborType(SyncSubscribeRepos_Identity{})
	cbornode.RegisterCborType(SyncSubscribeRepos_Account{})
	cbornode.RegisterCborType(SyncSubscribeRepos_Info{})
	cbornode.RegisterCborType(SyncSubscribeRepos_Sync{})
}

func ServerCreateSession(ctx context.Context, client lexutil.LexClient, input *ServerCreateSession_Input) (*ServerCreateSession_Output, error) {
	var out ServerCreateSession_Output
	if err := client.LexDo(ctx, lexutil.Procedure, "application/json", "com.atproto.server.createSession", nil, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func ServerCreateAccount(ctx context.Context, client lexutil.LexClient, input *ServerCreateAccount_Input) (*ServerCreateAccount_Output, error) {
	var out ServerCreateAccount_Output
	if err := client.LexDo(ctx, lexutil.Procedure, "application/json", "com.atproto.server.createAccount", nil, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func RepoCreateRecord(ctx context.Context, client lexutil.LexClient, input *RepoCreateRecord_Input) (*RepoCreateRecord_Output, error) {
	var out RepoCreateRecord_Output
	if err := client.LexDo(ctx, lexutil.Procedure, "application/json", "com.atproto.repo.createRecord", nil, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func RepoDeleteRecord(ctx context.Context, client lexutil.LexClient, input *RepoDeleteRecord_Input) (*RepoDeleteRecord_Output, error) {
	var out RepoDeleteRecord_Output
	if err := client.LexDo(ctx, lexutil.Procedure, "application/json", "com.atproto.repo.deleteRecord", nil, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func RepoGetRecord(ctx context.Context, client lexutil.LexClient, cid string, collection string, repo string, rkey string) (*RepoGetRecord_Output, error) {
	// Validate repo parameter (DID or Handle)
	if _, err := syntax.ParseDID(repo); err != nil {
		if _, err2 := syntax.ParseHandle(repo); err2 != nil {
			return nil, fmt.Errorf("repo must be valid DID or handle: %w", err)
		}
	}
	// Validate collection parameter (NSID)
	if _, err := syntax.ParseNSID(collection); err != nil {
		return nil, fmt.Errorf("collection must be valid NSID: %w", err)
	}
	// Validate rkey parameter (RecordKey)
	if _, err := syntax.ParseRecordKey(rkey); err != nil {
		return nil, fmt.Errorf("rkey must be valid record key: %w", err)
	}

	var out RepoGetRecord_Output
	params := map[string]any{
		"collection": collection,
		"repo":       repo,
		"rkey":       rkey,
	}
	if cid != "" {
		params["cid"] = cid
	}
	if err := client.LexDo(ctx, lexutil.Query, "", "com.atproto.repo.getRecord", params, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func SyncGetRepo(ctx context.Context, client lexutil.LexClient, did string, since string) ([]byte, error) {
	var out bytes.Buffer
	params := map[string]any{"did": did}
	if since != "" {
		params["since"] = since
	}
	if err := client.LexDo(ctx, lexutil.Query, "", "com.atproto.sync.getRepo", params, nil, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func SyncGetBlob(ctx context.Context, client lexutil.LexClient, cid string, did string) ([]byte, error) {
	var out bytes.Buffer
	if err := client.LexDo(ctx, lexutil.Query, "", "com.atproto.sync.getBlob", map[string]any{"cid": cid, "did": did}, nil, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func RepoUploadBlob(ctx context.Context, client lexutil.LexClient, input io.Reader) (*RepoUploadBlob_Output, error) {
	return RepoUploadBlobWithMime(ctx, client, "application/octet-stream", input)
}

func RepoUploadBlobWithMime(ctx context.Context, client lexutil.LexClient, mimeType string, input io.Reader) (*RepoUploadBlob_Output, error) {
	var out RepoUploadBlob_Output
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "application/octet-stream"
	}
	if err := client.LexDo(ctx, lexutil.Procedure, mimeType, "com.atproto.repo.uploadBlob", nil, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
