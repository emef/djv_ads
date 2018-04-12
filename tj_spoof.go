package djv_ads

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/publicsuffix"
)

type Session struct {
	client *http.Client
}

func NewSpoofedSession() (*Session, error) {
	username := os.Getenv("TJ_USERNAME")
	password := os.Getenv("TJ_PASSWORD")

	if username == "" || password == "" {
		return nil, errors.New("Missing username or password")
	}

	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Jar: jar,
	}

	loginResp, err := client.Get("https://www.trafficjunky.com/sign-in")
	if err != nil {
		return nil, err
	}
	defer loginResp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(loginResp.Body)
	if err != nil {
		return nil, err
	}

	token := ""
	doc.Find("input[name=_token]").Each(func(i int, s *goquery.Selection) {
		tokenValue, exists := s.Attr("value")
		if exists {
			token = tokenValue
		}
	})

	if token == "" {
		return nil, errors.New("Could not scrape csrf token")
	}

	loginForm := url.Values{}
	loginForm.Set("_token", token)
	loginForm.Set("username", username)
	loginForm.Set("password", password)

	loginPostResp, err := client.PostForm("https://www.trafficjunky.com/login", loginForm)
	if err != nil {
		return nil, err
	}
	defer loginPostResp.Body.Close()

	return &Session{client}, nil
}

func (session *Session) CurrentMaxTrafficBid(bidId, spotId string) (float64, error) {
	url := fmt.Sprintf("https://members.trafficjunky.com/campaign/viewbids/placementlist/?"+
		"placementId=%v&spotId=%v&countryCode=US&convertToReal=true", bidId, spotId)

	resp, err := session.client.Get(url)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()

	var jsonResp map[string]interface{}
	if err = json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		return 0, err
	}

	aaDataIface, ok := jsonResp["aaData"]
	aaData, ok := aaDataIface.([]interface{})
	if !ok {
		return 0, errors.New("aaData field has wrong type")
	}

	if len(aaData) == 0 {
		// not an error, just no data
		return 0, nil
	}

	row, ok := aaData[0].([]interface{})
	if !ok {
		return 0, errors.New("aaData row has wrong type")
	}

	bidStr, ok := row[1].(string)
	if !ok {
		return 0, errors.New("aaData row fields have wrong type")
	}

	bidStrParts := strings.Split(bidStr, "$")
	if len(bidStrParts) != 2 {
		return 0, errors.New(fmt.Sprintf("bidstr in incorrect format: %s", bidStr))
	}

	bidAmount, err := strconv.ParseFloat(bidStrParts[1], 64)
	if err != nil {
		return 0, err
	}

	return bidAmount, nil
}
