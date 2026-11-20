package config

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestDNSOverHTTPSQueriesBothFamilies(t *testing.T) {
	for _, test := range []struct {
		name        string
		wrongAnswer bool
	}{
		{name: "address lookup"},
		{name: "mismatched question", wrongAnswer: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			count := 0
			resolver := dnsOverHTTPS{
				endpoint: "https://dns.example/dns-query",
				client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					count++
					body, err := io.ReadAll(request.Body)
					if err != nil {
						return nil, err
					}
					var query dnsmessage.Message
					if err := query.Unpack(body); err != nil {
						return nil, err
					}
					if request.Method != http.MethodPost || request.Header.Get("Content-Type") != dnsContentType ||
						request.URL.RawQuery != "" || len(query.Questions) != 1 || query.Questions[0].Name.String() != "subscription.example." {
						return nil, fmt.Errorf("unexpected DNS request")
					}
					question := query.Questions[0]
					record := dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET}}
					switch question.Type {
					case dnsmessage.TypeA:
						record.Body = &dnsmessage.AResource{A: [4]byte{8, 8, 4, 4}}
					case dnsmessage.TypeAAAA:
						record.Body = &dnsmessage.AAAAResource{AAAA: netip.MustParseAddr("2606:4700::1111").As16()}
					default:
						return nil, fmt.Errorf("unexpected record type")
					}
					query.Response = true
					query.Answers = []dnsmessage.Resource{record}
					if test.wrongAnswer {
						query.Questions[0].Name = dnsmessage.MustNewName("unrelated.example.")
					}
					packet, err := query.Pack()
					if err != nil {
						return nil, err
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {dnsContentType}}, Body: io.NopCloser(bytes.NewReader(packet))}, nil
				})},
			}
			got, err := resolver.LookupNetIP(context.Background(), "ip", "subscription.example")
			if test.wrongAnswer {
				if err == nil {
					t.Fatal("resolver accepted a response to a different question")
				}
				return
			}
			want := []netip.Addr{netip.MustParseAddr("8.8.4.4"), netip.MustParseAddr("2606:4700::1111")}
			if err != nil || count != 2 || !slices.Equal(got, want) {
				t.Fatalf("got addresses %v, error %v and %d queries", got, err, count)
			}
		})
	}
}
