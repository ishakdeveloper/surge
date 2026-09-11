import { Detail, Details } from "@/components/app/details.js";
import { ActionError } from "@/components/app/errors.js";
import { Alert, AlertDescription } from "@/components/ui/alert.js";
import { Badge } from "@/components/ui/badge.js";
import { Button } from "@/components/ui/button.js";
import { Card } from "@/components/ui/card.js";
import { Heading, Text } from "@/components/ui/text.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { PositionUnavailable } from "@surge/client/Geolocation";
import { followAtom, followingAtom, shiftAtom } from "@surge/common/atom/driver-atoms";
import { connectionAtom } from "@surge/common/atom/realtime-atoms";
import { activeTripAtom } from "@surge/common/atom/trip-atoms";
import { statusFor } from "@surge/common/drive/driver-status";
import { formatPoint } from "@surge/common/lib/format";
import { DRIVER_SPOTS } from "@surge/common/ride/presets";
import { Cause, Option, Schema } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { View } from "react-native";

const isPositionUnavailable = Schema.is(PositionUnavailable);

/** What each refusal means for the person holding the phone. */
const positionMessage: Record<PositionUnavailable["reason"], string> = {
  Unsupported: "This device cannot report a position. Tap the map or choose a spot instead.",
  Denied: "Location permission was denied. Allow it in Settings, or tap the map instead.",
  Unavailable: "No position is available right now. Tap the map or choose a spot instead.",
  Timeout: "Finding your position took too long. Tap the map or choose a spot instead.",
};

/**
 * The shift: connection, position, and whether the driver wants work.
 *
 * The status sent to the matcher is shown as it is sent — `idle`, `offline`,
 * `enroute_pickup` — because it is derived, not chosen, and a driver wondering
 * why no offers arrive should be able to see what the system thinks they are.
 */
export const ShiftPanel = () => {
  const shift = useAtomValue(shiftAtom);
  const setShift = useAtomSet(shiftAtom);
  const connection = useAtomValue(connectionAtom);
  const following = useAtomValue(followingAtom);
  const setFollowing = useAtomSet(followingAtom);
  const tracking = useAtomValue(followAtom);
  const tripStatus = useAtomValue(
    activeTripAtom,
    (result) => Option.map(Option.flatten(AsyncResult.value(result)), (trip) => trip.status),
  );

  const connected = AsyncResult.isSuccess(connection) && connection.value === "Connected";
  const refusal = AsyncResult.isFailure(tracking)
    ? Option.filter(Cause.findErrorOption(tracking.cause), isPositionUnavailable)
    : Option.none();

  return (
    <Card>
      <View className="flex-row items-center justify-between">
        <Heading>Shift</Heading>
        <Badge variant={connected ? "default" : "outline"}>
          {connected ? "Connected" : "Connecting…"}
        </Badge>
      </View>

      <Details>
        <Detail
          label="Position"
          value={Option.match(shift.position, { onNone: () => "-", onSome: formatPoint })}
          mono
        />
        <Detail label="Status" value={statusFor(shift.online, tripStatus)} mono />
      </Details>

      {Option.match(refusal, {
        onSome: (error) => (
          <Alert>
            <AlertDescription>{positionMessage[error.reason]}</AlertDescription>
          </Alert>
        ),
        onNone: () =>
          AsyncResult.isFailure(tracking) ? <ActionError cause={tracking.cause} /> : null,
      })}

      <View className="gap-2">
        <Text className="text-xs text-muted-foreground">
          Or stand somewhere the matcher covers:
        </Text>
        <View className="flex-row flex-wrap gap-2">
          {DRIVER_SPOTS.map((spot) => (
            <Button
              key={spot.label}
              variant="outline"
              size="sm"
              onPress={() => {
                setFollowing(false);
                setShift({ ...shift, position: Option.some(spot.position) });
              }}
            >
              {spot.label}
            </Button>
          ))}
        </View>
      </View>

      <View className="flex-row gap-2">
        <Button
          className="flex-1"
          variant="outline"
          accessibilityState={{ checked: following }}
          onPress={() => {
            setFollowing(!following);
          }}
        >
          {following ? "Stop following GPS" : "Follow my GPS"}
        </Button>
        <Button
          className="flex-1"
          variant={shift.online ? "outline" : "default"}
          disabled={Option.isNone(shift.position)}
          onPress={() => {
            setShift({ ...shift, online: !shift.online });
          }}
        >
          {shift.online ? "Go offline" : "Go online"}
        </Button>
      </View>
    </Card>
  );
};
