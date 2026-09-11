import { ActionError } from "@/components/app/action-error.js";
import { Button } from "@/components/ui/button.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { answerOffer } from "@surge/common/atom/realtime-atoms";
import { secondsLeft } from "@surge/common/drive/driver-status";
import { formatPoint } from "@surge/common/lib/format";
import type { Offer } from "@surge/domain/realtime/Wire";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * Offers waiting for an answer, each with the time it has left.
 *
 * Handed the filtered list rather than deriving it, because the filter needs the
 * clock and the answered set as well, and the view that draws the map computes
 * exactly the same list for its pickup markers — two derivations of one list
 * would be two answers to "what is on offer".
 *
 * A live region, so an offer arriving is announced: it is the one event on this
 * page a driver cannot afford to miss, and it expires.
 */
export const OfferList = (
  props: { readonly offers: ReadonlyArray<Offer>; readonly now: number; readonly online: boolean; },
) => {
  const answering = useAtomValue(answerOffer);
  const answer = useAtomSet(answerOffer);

  return (
    <section className="flex flex-col gap-3" aria-labelledby="offers">
      <h2 id="offers" className="text-sm font-medium">Offers</h2>
      <div aria-live="polite" className="flex flex-col gap-2">
        {props.offers.length === 0 && (
          <p className="text-muted-foreground text-sm">
            {props.online ? "Waiting for a rider nearby…" : "Go online to receive offers."}
          </p>
        )}
        {props.offers.map((offer) => (
          <article
            key={offer.tripId}
            className="flex flex-col gap-2 rounded-md border border-border p-3"
          >
            <div className="flex items-baseline justify-between">
              <p className="text-sm font-medium">Pickup</p>
              <p className="tabular-nums text-sm">
                {secondsLeft(offer, props.now)}s
              </p>
            </div>
            <p className="font-mono text-xs">
              {formatPoint({ lat: offer.pickupLat, lng: offer.pickupLng })}
            </p>
            <p className="text-muted-foreground font-mono text-xs">{offer.tripId}</p>
            <div className="flex gap-2">
              <Button
                type="button"
                size="sm"
                disabled={answering.waiting}
                onClick={() => {
                  answer({ offer, accepted: true });
                }}
              >
                Accept
              </Button>
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={answering.waiting}
                onClick={() => {
                  answer({ offer, accepted: false });
                }}
              >
                Decline
              </Button>
            </div>
          </article>
        ))}
      </div>
      {AsyncResult.isFailure(answering) && <ActionError cause={answering.cause} />}
    </section>
  );
};
