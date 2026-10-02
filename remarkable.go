// remarkable.go: the reMarkable cloud side — auth and upload via ddvk/rmapi.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/juruen/rmapi/api"
	"github.com/juruen/rmapi/config"
	"github.com/juruen/rmapi/model"
	"github.com/juruen/rmapi/transport"
)

// deviceTokenEnv holds the durable device token produced by the auth subcommand.
// Only the device token is kept: the user token is derived from it per run
// (~2h TTL) and discarded.
const deviceTokenEnv = "REMARKABLE_DEVICE_TOKEN"

// registerDevice exchanges a one-time code for a durable device token.
// Mirrors the fork's current auth path (api.AuthHttpCtx -> newDeviceToken) and
// posts to config.NewTokenDevice. The legacy auth/ package must NOT be used:
// its private URL consts point at my.remarkable.com/token/..., a dead route.
// Codes come from my.remarkable.com (device pages, currently
// /device/remarkable?showOtp=true) and are single-use.
func registerDevice(code string) (string, error) {
	http := transport.CreateHttpClientCtx(model.AuthTokens{})
	req := model.DeviceTokenRequest{
		Code:       code,
		DeviceDesc: "desktop-windows",
		DeviceId:   uuid.New().String(),
	}
	resp := transport.BodyString{}
	if err := http.Post(transport.EmptyBearer, config.NewTokenDevice, req, &resp); err != nil {
		return "", fmt.Errorf("register device: %w", err)
	}
	if resp.Content == "" {
		return "", errors.New("register device: empty token returned")
	}
	return resp.Content, nil
}

// connect builds an API context from the env device token, mirroring main.go's
// flow: mint a user token, parse it (validates expiry + sync version), create
// the tree context. Note: sync15.CreateCtx reads and writes a tree cache at
// os.UserCacheDir()/rmapi/tree.cache (no override; ddvk/rmapi#80).
func connect() (api.ApiCtx, error) {
	dev := os.Getenv(deviceTokenEnv)
	if dev == "" {
		return nil, fmt.Errorf("no device token in %s — run: mdxrm auth <code>", deviceTokenEnv)
	}
	http := transport.CreateHttpClientCtx(model.AuthTokens{DeviceToken: dev})

	user := transport.BodyString{}
	if err := http.Post(transport.DeviceBearer, config.NewUserDevice, nil, &user); err != nil {
		return nil, fmt.Errorf("mint user token (device token revoked? re-run auth): %w", err)
	}
	http.Tokens.UserToken = user.Content

	info, err := api.ParseToken(user.Content)
	if err != nil {
		return nil, fmt.Errorf("parse user token: %w", err)
	}

	return api.CreateApiCtx(&http, info.SyncVersion)
}

// ensureDir returns the ID of the named folder at the cloud root, creating it
// if missing. The root's parent ID is the empty string.
func ensureDir(ctx api.ApiCtx, name string) (string, error) {
	root := ctx.Filetree().Root()
	if node, err := ctx.Filetree().NodeByPath(name, root); err == nil {
		if node.IsDirectory() {
			return node.Id(), nil
		}
		return "", fmt.Errorf("%q exists at root and is not a folder", name)
	}
	doc, err := ctx.CreateDir("", name, true)
	if err != nil {
		return "", fmt.Errorf("create folder %q: %w", name, err)
	}
	ctx.Filetree().AddDocument(doc)
	return doc.ID, nil
}

// uploadPDF uploads the PDF bytes under parent. UploadDocument takes the
// document's visible name from the file name (util.DocPathToName), so the temp
// file is named for the document. Creates a new document unconditionally —
// duplicates are acceptable, overwrites never happen.
func uploadPDF(ctx api.ApiCtx, parent, name string, pdf []byte) (string, error) {
	dir, err := os.MkdirTemp("", "mdxrm")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, name+".pdf")
	if err := os.WriteFile(path, pdf, 0o644); err != nil {
		return "", err
	}
	doc, err := ctx.UploadDocument(parent, path, true, nil, nil, nil, nil)
	if err != nil {
		return "", fmt.Errorf("upload %q: %w", name, err)
	}
	return doc.ID, nil
}
