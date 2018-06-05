package djv_ads

import (
	"sync"
	"time"

	"github.com/golang/glog"
	"golang.org/x/time/rate"
)

const TimeFormat = "2006-01-02 3:04pm"

var Pacific *time.Location

type Option func(*AdsController) error

type AdsController struct {
	campaignWhitelist []string
	readOnly          bool
	undercutAmount    float64
}

type BidUpdate struct {
	CampaignId  string
	BidId       string
	PreviousBid float64
	NewBid      float64
	Timestamp   string
}

func ReadOnly(readOnly bool) Option {
	return func(controller *AdsController) error {
		controller.readOnly = readOnly
		return nil
	}
}

func UndercutBy(amount float64) Option {
	return func(controller *AdsController) error {
		controller.undercutAmount = amount
		return nil
	}
}

func WithCampaignWhitelist(campaignIds ...string) Option {
	return func(controller *AdsController) error {
		controller.campaignWhitelist = campaignIds
		return nil
	}
}

func NewAdsController(opts ...Option) (*AdsController, error) {
	controller := &AdsController{}
	controller.undercutAmount = 0.001

	for _, opt := range opts {
		if err := opt(controller); err != nil {
			return nil, err
		}
	}

	return controller, nil
}

func (controller *AdsController) RunOnce() []*BidUpdate {
	rateLimiter := rate.NewLimiter(rate.Every(300*time.Millisecond), 1)

	session, err := NewSpoofedSession()
	if err != nil {
		glog.Errorf("Error creating spoofed TJ session: %v", err)
		return nil
	}

	var campaignIds []string
	if (len(controller.campaignWhitelist) > 0) {
		campaignIds = controller.campaignWhitelist
	} else {
		campaignIds, err = session.GetActiveCampaignIds()
		if err != nil {
			glog.Errorf("Error getting active campaign ID list: %v", err)
			return nil
		}
	}

	accountState, err := GetAccountState(campaignIds, session, rateLimiter)
	if err != nil {
		glog.Errorf("Error getting account state: %v", err)
		return nil
	}

	bidUpdates := controller.calculateNewBids(accountState)

	var wg sync.WaitGroup
	for _, update := range bidUpdates {
		glog.Infof("campaignId=%v bidId=%v prevBid=%v newBid=%v\n",
			update.CampaignId, update.BidId, update.PreviousBid, update.NewBid)

		if !controller.readOnly {
			wg.Add(1)
			go func() {
				if err := UpdateBid(update.BidId, update.NewBid, rateLimiter); err != nil {
					glog.Errorf("Error updating bid: %v\n", err)
				}

				wg.Done()
			}()
		}

		wg.Wait()
	}

	if !controller.readOnly {
		return bidUpdates
	} else {
		return nil
	}
}

func (controller *AdsController) calculateNewBids(
	accountState *AccountState) []*BidUpdate {

	updates := make([]*BidUpdate, 0)
	for _, campaign := range accountState.Campaigns {
		for _, bid := range campaign.Bids {
			maxBidForSpot := bid.CurrentMaxTrafficBid

			// Policy: no updates to spots with no current max bid
			if maxBidForSpot == 0 {
				continue
			}

			idealBid := maxBidForSpot - controller.undercutAmount

			// If our current bid is close enough to ideal, we don't update
			bidDelta := idealBid - bid.BidAmount
			if bidDelta >= 0 && bidDelta < 0.001 {
				continue
			}

			// Current bid needs to be updated
			update := &BidUpdate{
				CampaignId:  campaign.CampaignId,
				BidId:       bid.BidId,
				PreviousBid: bid.BidAmount,
				NewBid:      idealBid,
				Timestamp:   time.Now().In(Pacific).Format(TimeFormat),
			}

			updates = append(updates, update)
		}
	}

	return updates
}

func init() {
	Pacific, _ = time.LoadLocation("America/Los_Angeles")
}
