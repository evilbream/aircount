package tplink

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"poltergeist/internal/system"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

var (
	ErrSessionExpired = errors.New("tplink: session expired")
	ErrNotLoggedIn    = errors.New("tplink: not logged in")
	ErrUnauthorized   = errors.New("tplink: unauthorized")
)

type Client struct {
	host, username, password string
	http                     *http.Client
	aes                      aesCBC
	signKey                  rsaPub
	seq                      int
	hash                     string
	stok                     string
	sysauth                  string
	sessionPath              string // in ideal world its universal storage there and its interface, but im lazy so there is files
}

func New(host, username, password string) *Client {
	sessionStore, err := system.GetOrCreateTempFile("tplink-*.json")
	if err != nil {
		log.Error().Err(err).Msg("failed to access filesystem to create or get temp file")
	}

	return &Client{
		host:        host,
		username:    username,
		password:    password,
		http:        &http.Client{Timeout: 10 * time.Second},
		sessionPath: sessionStore,
	}
}

type envelope struct {
	Success   bool            `json:"success"`
	ErrorCode any             `json:"errorcode"`
	Data      json.RawMessage `json:"data"`
}

func (c *Client) url(path string) string {
	return c.host + "/cgi-bin/luci/;stok=" + c.stok + "/" + path
}

func (c *Client) Login(ctx context.Context) error {
	loadErr := c.loadSession()

	if loadErr != nil {
		log.Error().Err(loadErr).Msg("tplink: failed to load session")
	}

	if loadErr == nil {
		if c.ping(ctx) == nil {
			log.Debug().Str("session", c.sessionPath).Msg("tplink: session valid")
			return nil
		}
		log.Debug().Str("session", c.sessionPath).Msg("tplink: session expired, logging in")
	}

	if err := c.login(ctx); err != nil {
		return err
	}
	log.Debug().Str("session", c.sessionPath).Msg("tplink: login successful")
	if err := c.saveSession(); err != nil {
		log.Error().Err(err).Msg("tplink: failed to save session")
	}
	log.Debug().Str("session", c.sessionPath).Msg("tplink: session saved")
	return nil
}

func (c *Client) login(ctx context.Context) error {
	var keys struct {
		Password []string `json:"password"`
	}
	if err := c.plain(ctx, "login?form=keys&operation=read", "", &keys); err != nil {
		return fmt.Errorf("tplink: form=keys: %w", err)
	}
	var auth struct {
		Key []string `json:"key"`
		Seq int      `json:"seq"`
	}
	if err := c.plain(ctx, "login?form=auth&operation=read", "", &auth); err != nil {
		return fmt.Errorf("tplink: form=auth: %w", err)
	}
	if len(keys.Password) != 2 || len(auth.Key) != 2 {
		return errors.New("tplink: unexpected key format")
	}
	pwdKey, err := parseRSA(keys.Password[0], keys.Password[1])
	if err != nil {
		return err
	}
	if c.signKey, err = parseRSA(auth.Key[0], auth.Key[1]); err != nil {
		return err
	}
	c.seq = auth.Seq
	sum := md5.Sum([]byte(c.username + c.password))
	c.hash = hex.EncodeToString(sum[:])
	c.aes = aesCBC{key: randomHex16(), iv: randomHex16()}
	c.stok, c.sysauth = "", ""

	encPwd, err := pwdKey.encryptHex([]byte(c.password))
	if err != nil {
		return err
	}
	payload := "operation=login&password=" + encPwd + "&confirm=true"
	data, err := c.do(ctx, "login?form=login", []byte(payload), true)
	if err != nil {
		return fmt.Errorf("tplink: login: %w", err)
	}
	var res struct {
		Stok string `json:"stok"`
	}
	if err := json.Unmarshal(data, &res); err != nil || res.Stok == "" {
		return fmt.Errorf("tplink: login: no stok in response")
	}
	c.stok = res.Stok
	return nil
}

func (c *Client) Logout(ctx context.Context) error {
	if c.stok == "" {
		return ErrNotLoggedIn
	}
	defer func() { c.stok, c.sysauth = "", "" }()
	_, err := c.do(ctx, "admin/system?form=logout&operation=write", []byte(`{"operation":"write"}`), false)
	return err
}

func (c *Client) ping(ctx context.Context) error {
	var r json.RawMessage
	if err := c.callOnce(ctx, "admin/status?form=all", "read", &r); err != nil {
		return err
	}
	return nil
}

// call: a JSON payload {"operation": op}, encrypted and signed; the decrypted data goes into out.
func (c *Client) callOnce(ctx context.Context, endpoint, op string, out any) error {
	if c.stok == "" {
		return ErrNotLoggedIn
	}
	payload, _ := json.Marshal(map[string]string{"operation": op})
	data, err := c.do(ctx, endpoint+"&operation="+op, payload, false)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// call: a JSON payload {"operation": op}, encrypted and signed; the decrypted data goes into out.
func (c *Client) call(ctx context.Context, endpoint, op string, out any) error {
	err := c.callOnce(ctx, endpoint, op, out)

	if !errors.Is(err, ErrSessionExpired) && !errors.Is(err, ErrNotLoggedIn) && !errors.Is(err, ErrUnauthorized) {
		return err
	}
	log.Debug().Str("session", c.sessionPath).Msg("tplink: session lost, logging in")
	if err := c.login(ctx); err != nil {
		return err
	}
	if err := c.saveSession(); err != nil {
		log.Error().Err(err).Msg("tplink: failed to save session")
	}
	return c.callOnce(ctx, endpoint, op, out)
}

func (c *Client) do(ctx context.Context, path string, payload []byte, login bool) (json.RawMessage, error) {
	enc, err := c.aes.encrypt(payload)
	if err != nil {
		return nil, fmt.Errorf("tplink: aes encrypt: %w", err)
	}
	sig, err := c.sign(len(enc), login)
	if err != nil {
		return nil, fmt.Errorf("tplink: sign: %w", err)
	}
	body := "sign=" + url.QueryEscape(sig) + "&data=" + url.QueryEscape(enc)
	raw, cookies, err := c.post(ctx, path, body)
	if err != nil {
		return nil, fmt.Errorf("tplink: post: %w", err)
	}
	if login {
		for _, ck := range cookies {
			if ck.Name == "sysauth" {
				c.sysauth = ck.Value
			}
		}
	}
	var outer struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil {
		return nil, fmt.Errorf("tplink: bad response: %w", err)
	}
	plain, err := c.aes.decrypt(outer.Data)
	if err != nil {
		return nil, err
	}
	return unwrap(plain)
}

func (c *Client) plain(ctx context.Context, path, body string, out any) error {
	raw, _, err := c.post(ctx, path, body)
	if err != nil {
		return err
	}
	data, err := unwrap(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (c *Client) post(ctx context.Context, path, body string) ([]byte, []*http.Cookie, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), strings.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.host+"/webpages/index.html")
	req.Header.Set("Origin", c.host)
	if c.sysauth != "" {
		req.AddCookie(&http.Cookie{Name: "sysauth", Value: c.sysauth})
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return nil, nil, ErrUnauthorized
	case resp.StatusCode != http.StatusOK:
		return nil, nil, fmt.Errorf("tplink: http %d", resp.StatusCode)
	}
	return raw, resp.Cookies(), nil
}

type session struct {
	Stok    string `json:"stok"`
	Sysauth string `json:"sysauth"`
	AESKey  string `json:"aes_key"`
	AESIV   string `json:"aes_iv"`
	SignN   string `json:"sign_n"` // hex
	SignE   string `json:"sign_e"` // hex
	Seq     int    `json:"seq"`
}

func (c *Client) saveSession() error {
	b, err := json.Marshal(session{
		Stok: c.stok, Sysauth: c.sysauth,
		AESKey: c.aes.key, AESIV: c.aes.iv,
		SignN: c.signKey.n.Text(16), SignE: c.signKey.e.Text(16),
		Seq: c.seq,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(c.sessionPath, b, 0o600) // в файле токен и ключ, доступ только владельцу
}

func (c *Client) loadSession() error {
	b, err := os.ReadFile(c.sessionPath)
	if err != nil {
		return err
	}
	var s session
	if len(b) == 0 || json.Unmarshal(b, &s) != nil || s.Stok == "" {
		return ErrNotLoggedIn // файл только что создан или битый
	}
	key, err := parseRSA(s.SignN, s.SignE)
	if err != nil {
		return err
	}
	c.stok, c.sysauth, c.seq, c.signKey = s.Stok, s.Sysauth, s.Seq, key
	c.aes = aesCBC{key: s.AESKey, iv: s.AESIV}
	sum := md5.Sum([]byte(c.username + c.password))
	c.hash = hex.EncodeToString(sum[:])
	return nil
}

func unwrap(raw []byte) (json.RawMessage, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("tplink: bad envelope: %w", err)
	}
	if !env.Success {
		return nil, fmt.Errorf("tplink: router error %v", env.ErrorCode)
	}
	return env.Data, nil
}
