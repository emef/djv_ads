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
	"time"

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

func (session *Session) GetActiveCampaignIds() ([]string, error) {
	dateFmt := "2006-01-02"
	startDate := time.Now().Add(-28 * 24 * time.Hour).In(Pacific).Format(dateFmt)
	endDate := time.Now().In(Pacific).Format(dateFmt)

	url := fmt.Sprintf("https://members.trafficjunky.com/campaign/ajaxlistv5?sEcho=1&iColumns=18&sColumns=id,campaignStatusString,id,campaignPermissionList,name,cookieTargeting,adCount,placementCount,daily_budget,daily_budget_left_display,impressions,clicks,ctr,conversions,cost,ecpm,ecpc,jsonLabels&iDisplayStart=0&iDisplayLength=25&mDataProp_0=id&sSearch_0=&bRegex_0=false&bSearchable_0=true&bSortable_0=false&mDataProp_1=campaignStatusString&sSearch_1=&bRegex_1=false&bSearchable_1=true&bSortable_1=false&mDataProp_2=id&sSearch_2=&bRegex_2=false&bSearchable_2=true&bSortable_2=true&mDataProp_3=campaignPermissionList&sSearch_3=&bRegex_3=false&bSearchable_3=true&bSortable_3=false&mDataProp_4=name&sSearch_4=&bRegex_4=false&bSearchable_4=true&bSortable_4=true&mDataProp_5=cookieTargeting&sSearch_5=&bRegex_5=false&bSearchable_5=true&bSortable_5=false&mDataProp_6=adCount&sSearch_6=&bRegex_6=false&bSearchable_6=true&bSortable_6=false&mDataProp_7=placementCount&sSearch_7=&bRegex_7=false&bSearchable_7=true&bSortable_7=false&mDataProp_8=daily_budget&sSearch_8=&bRegex_8=false&bSearchable_8=true&bSortable_8=true&mDataProp_9=daily_budget_left_display&sSearch_9=&bRegex_9=false&bSearchable_9=true&bSortable_9=true&mDataProp_10=impressions&sSearch_10=&bRegex_10=false&bSearchable_10=true&bSortable_10=true&mDataProp_11=clicks&sSearch_11=&bRegex_11=false&bSearchable_11=true&bSortable_11=true&mDataProp_12=ctr&sSearch_12=&bRegex_12=false&bSearchable_12=true&bSortable_12=true&mDataProp_13=conversions&sSearch_13=&bRegex_13=false&bSearchable_13=true&bSortable_13=true&mDataProp_14=cost&sSearch_14=&bRegex_14=false&bSearchable_14=true&bSortable_14=true&mDataProp_15=ecpm&sSearch_15=&bRegex_15=false&bSearchable_15=true&bSortable_15=true&mDataProp_16=ecpc&sSearch_16=&bRegex_16=false&bSearchable_16=true&bSortable_16=true&mDataProp_17=jsonLabels&sSearch_17=&bRegex_17=false&bSearchable_17=true&bSortable_17=false&sSearch=&bRegex=false&iSortCol_0=2&sSortDir_0=desc&iSortingCols=1&formURL=startDate=%v&endDate=%v&isDashboard=true&formJSON={\"startDate\":\"%v\",\"endDate\":\"%v\",\"isDashboard\":\"true\"}", startDate, endDate, startDate, endDate)

	resp, err := session.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var jsonResp map[string]interface{}
	if err = json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		return nil, err
	}

	aaDataIface, ok := jsonResp["aaData"]
	aaData, ok := aaDataIface.([]interface{})
	if !ok {
		return nil, errors.New("aaData field has wrong type")
	}

	if len(aaData) == 0 {
		// not an error, just no data
		return nil, nil
	}

	campaignIds := make([]string, 0)
	for i, rowIface := range aaData {
		rowMap, ok := rowIface.(map[string]interface{})
		if !ok {
			return nil, errors.New(fmt.Sprintf("aaData[%v] row has wrong type", i))
		}

		campaignIdIface, _ := rowMap["id"]
		statusIface, _ := rowMap["status"]

		campaignId, ok := campaignIdIface.(float64)
		if !ok {
			return nil, errors.New(fmt.Sprintf("campaignId[%v] is wrong type: %v", i, campaignIdIface))
		}

		status, ok := statusIface.(string)
		if !ok {
			return nil, errors.New(fmt.Sprintf("status[%v] is wrong type", i))
		}

		if status == "active" {
			campaignIds = append(campaignIds, strconv.Itoa(int(campaignId)))
		}
	}

	return campaignIds, nil
}
