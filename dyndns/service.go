package dyndns

import (
	"bufio"
	"bytes"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/cloudflare/cloudflare-go"
)

func UpdateDNS(domain, zone, token string) error {
	api, err := cloudflare.NewWithAPIToken(token)
	if err != nil {
		return err
	}

	up := updater{api: api, zone: zone}

	if err := up.loadRecords(domain); err != nil {
		return err
	}

	return up.updateRecords()
}

func SyncDNS(domain, zone, token string, update time.Duration) error {
	api, err := cloudflare.NewWithAPIToken(token)
	if err != nil {
		return err
	}

	up := updater{api: api, zone: zone}

	if err := up.loadRecords(domain); err != nil {
		return err
	}

	for {
		if err := up.updateRecords(); err != nil {
			log.Println("failed to update DNS records:", err)
		}
		time.Sleep(update)
	}
}

type updater struct {
	api        *cloudflare.API
	zone       string
	a, aaaa    string
	ipv4, ipv6 string
}

func (up *updater) loadRecords(domain string) error {
	recs, err := up.api.DNSRecords(up.zone, cloudflare.DNSRecord{Name: domain})
	if err != nil {
		return err
	}

	for i := range recs {
		switch recs[i].Type {
		case "A":
			if up.a != "" {
				return errors.New("Multiple A records found for " + domain)
			}
			up.a = recs[i].ID
			up.ipv4 = recs[i].Content
		case "AAAA":
			if up.aaaa != "" {
				return errors.New("Multiple AAAA records found for " + domain)
			}
			up.aaaa = recs[i].ID
			up.ipv6 = recs[i].Content
		}
	}
	if up.a == "" && up.aaaa == "" {
		return errors.New("No A/AAAA records found for " + domain)
	}

	return nil
}

func (up *updater) updateRecords() (err error) {
	if up.a != "" {
		ip, e := PublicIPv4()
		if e == nil && ip != up.ipv4 {
			e = up.updateRecord(up.a, ip)
		}
		if e == nil {
			up.ipv4 = ip
		} else {
			err = e
		}
	}

	if up.aaaa != "" {
		ip, e := PublicIPv6()
		if e == nil && ip != up.ipv6 {
			e = up.updateRecord(up.aaaa, ip)
		}
		if e == nil {
			up.ipv6 = ip
		} else {
			err = e
		}
	}

	return
}

func (up *updater) updateRecord(record, content string) error {
	rec, err := up.api.DNSRecord(up.zone, record)
	if err == nil && rec.Content != content {
		rec.Content = content
		_, err = up.api.Raw("PATCH", "/zones/"+up.zone+"/dns_records/"+record, rec)
	}
	return err
}

func PublicIPv4() (string, error) {
	return publicIP("1.1.1.1", "1.0.0.1")
}

func PublicIPv6() (string, error) {
	return publicIP("[2606:4700:4700::1111]", "[2606:4700:4700::1001]")
}

func publicIP(primary, secondary string) (string, error) {
	res, err := http.Get("https://" + primary + "/cdn-cgi/trace")
	if err != nil {
		res, err = http.Get("https://" + secondary + "/cdn-cgi/trace")
	}
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", errors.New(http.StatusText(res.StatusCode))
	}

	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		const prefix = "ip="
		if bytes.HasPrefix(scanner.Bytes(), []byte(prefix)) {
			return string(scanner.Bytes()[len(prefix):]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("parse error: ip not found")
}