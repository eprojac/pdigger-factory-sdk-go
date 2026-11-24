// Command rawpackets follows the model's raw output on a unit, before the world: one sip1 packet per frame, with
// the model's stone detections and how long the model took. It observes, so it keeps nothing running on the unit.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./rawpackets
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/codec"
)

func main() {
	ctx := context.Background()
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"))
	if err != nil {
		log.Fatal(err)
	}
	packets := 0
	for item, err := range client.Streams().RawPackets(ctx, pdiggerfactory.Observe(true)) {
		if err != nil {
			log.Fatal(err)
		}
		p, ok := item.(*pdiggerfactory.PacketItem)
		if !ok {
			continue // a gap: packets were skipped
		}
		stones := codec.StoneItems(p.Packet.Stones.Items) // tuples [u, v, w, h, score, X, Y, Z, w_m, h_m, yaw]
		fmt.Printf("g %d: %d stones detected, the model took %s ms (encoder %s ms)\n", p.Packet.G, len(stones),
			pdiggerfactory.FormatFixed(p.Packet.MS["total"], 1), pdiggerfactory.FormatFixed(p.Packet.MS["encoder"], 1))
		if packets++; packets == 5 {
			break // closes the connection
		}
	}
}
