package djv_ads

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"golang.org/x/time/rate"
)

type AccountState struct {
	Campaigns map[string]*Campaign
}

type Campaign struct {
	CampaignId string
	Name       string
	IsActive   bool
	Bids       map[string]*Bid
}

type Bid struct {
	BidId                string
	BidAmount            float64
	SpotId               string
	IsActive             bool
	CurrentMaxTrafficBid float64
}

type BidsResponseJson struct {
	BidMap map[int32]*BidJson `json:"bids,omitempty"`
}

type BidJson struct {
	BidId     string `json:"bid_id,omitempty"`
	BidAmount string `json:"bid,omitempty"`
	SpotId    string `json:"spot_id,omitempty"`
	IsActive  bool   `json:"isActive,omitempty"`
	IsPaused  int32  `json:"isPaused,omitempty"`
}

type CampaignJson struct {
	CampaignId int32  `json:"campaign_id,omitempty"`
	Name       string `json:"campaign_name,omitempty"`
	Status     string `json:"status,omitempty"`
	EndDate    string `json:"end_date,omitempty"`
}

type HTTPError struct {
	Url        string
	StatusCode int
	Status     string
}

func UpdateBid(bidId string, newBidAmount float64, rateLimiter *rate.Limiter) error {
	rateLimiter.Wait(context.Background())
	baseUrl := fmt.Sprintf("https://api.trafficjunky.com/api/bids/%v/set.json", bidId)
	url, err := formatUrl(
		baseUrl,
		"bid", strconv.FormatFloat(newBidAmount, 'f', 4, 64))

	if err != nil {
		return err
	}

	resp, err := makeRequest(url, "PUT")
	if err != nil {
		return err
	}

	resp.Close()
	return nil
}

func GetAccountState(
	campaignIdWhitelist []string,
	session *Session,
	rateLimiter *rate.Limiter) (*AccountState, error) {

	campaignJsons, err := GetAllCampaigns(rateLimiter)
	if err != nil {
		return nil, err
	}

	campaignChan := make(chan *Campaign, len(campaignJsons))
	var wg sync.WaitGroup
	for _, campaignJson := range campaignJsons {
		campaignId := strconv.Itoa(int(campaignJson.CampaignId))

		// whitelist enabled when non-empty
		if len(campaignIdWhitelist) > 0 {
			found := false
			for _, whitelistedCampaignId := range campaignIdWhitelist {
				if campaignId == whitelistedCampaignId {
					found = true
					break
				}
			}

			if !found {
				continue
			}
		}

		campaignName := campaignJson.Name
		campaignIsActive := determineCampaignIsActive(campaignJson)

		wg.Add(1)
		go func() {
			defer wg.Done()

			// TODO: fetch campaign info and skip the rest of the processing
			// if endDate has passed (the endDate we get from the all-campaigns
			// call above does not work).

			bids := make(map[string]*Bid)
			bidRespJson, err := GetBidsForCampaign(campaignId, rateLimiter)
			glog.Infof("Got bids for campaign %s", campaignId)
			if err == nil {
				for _, bidJson := range bidRespJson.BidMap {
					bidIsActive := bidJson.IsActive && bidJson.IsPaused == 0
					bidAmount, err := strconv.ParseFloat(bidJson.BidAmount, 64)
					if err != nil {
						glog.Errorf("Error parsing bidAmount: %s", bidJson.BidAmount)
						continue
					}

					currentMaxTrafficBid := float64(0.0)
					if bidIsActive {
						rateLimiter.Wait(context.Background())
						currentMaxTrafficBid, err = session.CurrentMaxTrafficBid(
							bidJson.BidId, bidJson.SpotId)
						if err != nil {
							glog.Errorf(
								"Error retrieving current max top bid for campaignId=%v bidId=%v spotId=%v",
								campaignId, bidJson.BidId, bidJson.SpotId)
						}
					}

					bid := &Bid{
						BidId:                bidJson.BidId,
						BidAmount:            bidAmount,
						SpotId:               bidJson.SpotId,
						IsActive:             bidIsActive,
						CurrentMaxTrafficBid: currentMaxTrafficBid,
					}

					bids[bidJson.BidId] = bid
				}
			}

			glog.Infof("Done processing campaign %s", campaignId)

			campaign := &Campaign{
				CampaignId: campaignId,
				Name:       campaignName,
				IsActive:   campaignIsActive,
				Bids:       bids}

			campaignChan <- campaign
		}()
	}

	wg.Wait()
	close(campaignChan)

	campaigns := make(map[string]*Campaign)
	for campaign := range campaignChan {
		campaigns[campaign.CampaignId] = campaign
	}

	return &AccountState{
		Campaigns: campaigns,
	}, nil
}

func GetAllCampaigns(rateLimiter *rate.Limiter) ([]*CampaignJson, error) {
	rateLimiter.Wait(context.Background())
	url, err := formatUrl(
		"https://api.trafficjunky.com/api/campaigns.json",
		"maxResults", "300")
	if err != nil {
		return nil, err
	}

	resp, err := makeRequest(url, "GET")
	if err != nil {
		return nil, err
	}

	defer resp.Close()

	campaigns := make([]*CampaignJson, 0)
	dec := json.NewDecoder(resp)
	if err := dec.Decode(&campaigns); err != nil {
		return nil, err
	}

	return campaigns, nil
}

func GetBidsForCampaign(
	campaignId string,
	rateLimiter *rate.Limiter) (*BidsResponseJson, error) {

	rateLimiter.Wait(context.Background())
	baseUrl := fmt.Sprintf("https://api.trafficjunky.com/api/bids/%v.json", campaignId)
	url, err := formatUrl(baseUrl)
	if err != nil {
		return nil, err
	}

	resp, err := makeRequest(url, "GET")
	if err != nil {
		return nil, err
	}

	defer resp.Close()

	bids := &BidsResponseJson{}
	dec := json.NewDecoder(resp)
	if err := dec.Decode(bids); err != nil {
		return nil, err
	}

	return bids, nil
}

func determineCampaignIsActive(campaignJson *CampaignJson) bool {
	if campaignJson.Status != "active" {
		return false
	}

	layout := "2006-01-02T15:04:05-0700"
	endDate, err := time.Parse(layout, campaignJson.EndDate)
	if err != nil {
		// endate malformed or missing, assume it's running
		return true
	} else {
		glog.Infof("%v enddate is %v (parsed %v)",
			campaignJson.CampaignId, campaignJson.EndDate, endDate)
	}

	return endDate.After(time.Now())
}

func formatUrl(baseUrl string, extraParams ...string) (string, error) {
	if len(extraParams)%2 != 0 {
		return "", errors.New("Wrong number of param key/value pairs")
	}

	url, err := url.Parse(baseUrl)
	if err != nil {
		return "", err
	}

	query := url.Query()
	query.Set("api_key", apiKey())

	numKeys := len(extraParams) / 2
	for i := 0; i < numKeys; i++ {
		query.Set(extraParams[i*2], extraParams[i*2+1])
	}

	url.RawQuery = query.Encode()
	return url.String(), nil
}

func apiKey() string {
	return os.Getenv("TJ_API_KEY")
}

func makeRequest(url string, method string) (io.ReadCloser, error) {
	client := &http.Client{}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	useragent := "Chrome"
	req.Header.Set("User-Agent", useragent)
	resp, err := client.Do(req)

	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close();
		return nil, HTTPError{
			Url:        url,
			StatusCode: resp.StatusCode,
			Status:     resp.Status}
	}

	return resp.Body, nil
}

func (err HTTPError) Error() string {
	return fmt.Sprintf("HttpError %v: %v from %v",
		err.StatusCode,
		err.Status,
		err.Url)
}
