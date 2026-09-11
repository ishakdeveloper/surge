import { Announced } from "@/components/app/announced.js";
import { ActionError } from "@/components/app/errors.js";
import { Button } from "@/components/ui/button.js";
import { Card } from "@/components/ui/card.js";
import { Heading, Text } from "@/components/ui/text.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { answerOffer } from "@surge/common/atom/realtime-atoms";
import { secondsLeft } from "@surge/common/drive/driver-status";
import { formatPoint } from "@surge/common/lib/format";
import type { Offer } from "@surge/domain/realtime/Wire";
import { AsyncResult } from "effect/unstable/reactivity";
import { View } from "react-native";

/**
 * Offers waiting for an answer, each with the time it has left.
 *
 * Announced, because an offer arriving is the one event a driver cannot afford
 * to miss, and it expires.
 */
export const OfferList = (props: {
  readonly offers: ReadonlyArray<Offer>;
  readonly now: number;
  readonly online: boolean;
}) => {
  const answering = useAtomValue(answerOffer);
  const answer = useAtomSet(answerOffer);

  const summary = props.offers.length === 0
    ? props.online ? "Waiting for a rider nearby…" : "Go online to receive offers."
    : props.offers.length === 1
    ? "One offer waiting."
    : `${props.offers.length} offers waiting.`;

  return (
    <View className="gap-3">
      <Heading>Offers</Heading>
      <Announced message={summary} className="gap-2">
        {props.offers.length === 0 && <Text className="text-muted-foreground">{summary}</Text>}
        {props.offers.map((offer) => (
          <Card key={offer.tripId} className="gap-2 p-3">
            <View className="flex-row items-baseline justify-between">
              <Text className="font-medium">Pickup</Text>
              <Text className="tabular-nums">{secondsLeft(offer, props.now)}s</Text>
            </View>
            <Text className="font-mono text-xs">
              {formatPoint({ lat: offer.pickupLat, lng: offer.pickupLng })}
            </Text>
            <Text className="font-mono text-xs text-muted-foreground">{offer.tripId}</Text>
            <View className="flex-row gap-2">
              <Button
                className="flex-1"
                disabled={answering.waiting}
                onPress={() => {
                  answer({ offer, accepted: true });
                }}
              >
                Accept
              </Button>
              <Button
                className="flex-1"
                variant="outline"
                disabled={answering.waiting}
                onPress={() => {
                  answer({ offer, accepted: false });
                }}
              >
                Decline
              </Button>
            </View>
          </Card>
        ))}
      </Announced>
      {AsyncResult.isFailure(answering) && <ActionError cause={answering.cause} />}
    </View>
  );
};
