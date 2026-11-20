package config

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const dnsContentType = "application/dns-message"
const maxDNSPacketSize = 65535

// The public IP in this endpoint avoids a system DNS lookup for the resolver itself.
var publicDNS = dnsOverHTTPS{
	endpoint: "https://1.1.1.1/dns-query",
	client: &http.Client{
		Timeout: 4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
			ForceAttemptHTTP2:      true,
			MaxResponseHeaderBytes: 64 << 10,
			IdleConnTimeout:        30 * time.Second,
		},
	},
}

type dnsOverHTTPS struct {
	endpoint string
	client   *http.Client
}

func (r dnsOverHTTPS) LookupNetIP(ctx context.Context, _, host string) ([]netip.Addr, error) {
	name, err := dnsmessage.NewName(strings.TrimSuffix(host, ".") + ".")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var addresses []netip.Addr
	for _, kind := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		found, err := r.lookup(ctx, dnsmessage.Question{Name: name, Type: kind, Class: dnsmessage.ClassINET})
		if err != nil {
			return nil, err
		}
		addresses = append(addresses, found...)
	}
	return addresses, nil
}

func (r dnsOverHTTPS) lookup(ctx context.Context, question dnsmessage.Question) ([]netip.Addr, error) {
	query := dnsmessage.Message{
		Header:    dnsmessage.Header{RecursionDesired: true},
		Questions: []dnsmessage.Question{question},
	}
	packet, err := query.Pack()
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(packet))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", dnsContentType)
	request.Header.Set("Content-Type", dnsContentType)
	response, err := r.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || mediaType != dnsContentType {
		return nil, errors.New("unexpected DNS-over-HTTPS response")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDNSPacketSize+1))
	if err != nil {
		return nil, err
	}
	var answer dnsmessage.Message
	if len(body) > maxDNSPacketSize || answer.Unpack(body) != nil || !answer.Response || answer.Truncated ||
		answer.ID != query.ID || answer.RCode != dnsmessage.RCodeSuccess || len(answer.Questions) != 1 ||
		answer.Questions[0].Type != question.Type || answer.Questions[0].Class != question.Class ||
		!strings.EqualFold(answer.Questions[0].Name.String(), question.Name.String()) {
		return nil, errors.New("invalid DNS-over-HTTPS message")
	}

	var addresses []netip.Addr
	for _, record := range answer.Answers {
		if record.Header.Class != dnsmessage.ClassINET || record.Header.Type != question.Type {
			continue
		}
		switch data := record.Body.(type) {
		case *dnsmessage.AResource:
			addresses = append(addresses, netip.AddrFrom4(data.A))
		case *dnsmessage.AAAAResource:
			addresses = append(addresses, netip.AddrFrom16(data.AAAA))
		}
	}
	return addresses, nil
}
