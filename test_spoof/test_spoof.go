package main

import (
	"fmt"

	. "github.com/emef/djv_ads"
)

func main() {
	session, err := NewSpoofedSession()
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}

	topBid, err := session.CurrentMaxTrafficBid("1039854091", "32")
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}

	fmt.Printf("top bid: %v\n", topBid)
}
