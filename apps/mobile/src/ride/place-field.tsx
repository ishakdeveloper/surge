import type { MapPoint } from "@/components/map/surge-map.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import type { Place } from "@surge/client/Places";
import {
  placeAtAtom,
  type PlaceField as Field,
  placeQueryAtom,
  placeResultsAtom,
  PointKey,
} from "@surge/common/atom/place-atoms";
import { formatPoint } from "@surge/common/lib/format";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";
import { Pressable, TextInput, View } from "react-native";
import Animated, {
  Easing,
  FadeInDown,
  useAnimatedStyle,
  useSharedValue,
  withTiming,
} from "react-native-reanimated";

/**
 * A stop: exactly where the car should go, and the place it is when the rider
 * chose one by name. A point tapped on the map has no name yet; `StopName`
 * looks one up.
 */
export interface Stop {
  readonly position: MapPoint;
  readonly place: Option.Option<Place>;
}

/** A stop's name as a rider reads it — a string, so it can sit inside any `Text`. */
export const StopName = (props: { readonly stop: Stop; }) =>
  Option.match(props.stop.place, {
    onSome: (place) => <>{place.name}</>,
    onNone: () => <PointName point={props.stop.position} />,
  });

/**
 * The address nearest a point. When search is down the point is still exactly
 * where the rider put it, so it is shown as a position rather than hidden.
 */
const PointName = (props: { readonly point: MapPoint; }) => {
  const place = useAtomValue(placeAtAtom(PointKey.make(props.point)));

  if (AsyncResult.isSuccess(place)) {
    return Option.match(place.value, {
      onNone: () => <>Pin on the map</>,
      onSome: (found) => <>{found.name}</>,
    });
  }
  if (AsyncResult.isFailure(place)) return <>{formatPoint(props.point)}</>;
  return <>Finding the address…</>;
};

/**
 * Where a stop sits on the rail: the pickup a yellow disc ringed in ink, the
 * dropoff an ink square with a yellow heart — the marks the map stands at the
 * two ends of the route.
 */
export const StopDot = (props: { readonly kind: Field; }) =>
  props.kind === "pickup"
    ? (
      <View className="size-4 items-center justify-center rounded-full border-[3px] border-foreground bg-primary">
        <View className="size-1.5 rounded-full bg-foreground" />
      </View>
    )
    : (
      <View className="size-4 items-center justify-center rounded-[5px] bg-foreground">
        <View className="size-1.5 rounded-[2px] bg-primary" />
      </View>
    );

/** A stop on the rail: its mark, what it is, and where. */
const StopLine = (props: {
  readonly kind: Field;
  readonly label: string;
  readonly children: React.ReactNode;
}) => (
  <>
    <View className="w-5 items-center">
      <StopDot kind={props.kind} />
    </View>
    <View className="min-w-0 flex-1">
      <Text className="text-xs font-semibold text-muted-foreground">{props.label}</Text>
      <Text numberOfLines={1} className="text-base font-semibold">{props.children}</Text>
    </View>
  </>
);

/** A stop that is read, not edited: the trip card's. */
export const StopRow = (props: {
  readonly kind: Field;
  readonly label: string;
  readonly stop: Stop;
}) => (
  <View className="flex-row items-center gap-3 px-3 py-2.5">
    <StopLine kind={props.kind} label={props.label}>
      <StopName stop={props.stop} />
    </StopLine>
  </View>
);

/**
 * The trip's two stops on one grey card, joined by a rail of dots the way a
 * route map joins its stations — dots rather than a dotted border, which iOS
 * will not draw on one side. The rail goes while a stop is being edited,
 * because the places found open between the two.
 */
export const StopsCard = (props: {
  readonly rail: boolean;
  readonly footer: React.ReactNode;
  readonly children: React.ReactNode;
}) => (
  <View className="rounded-2xl bg-tile p-1.5">
    <View className="relative gap-0.5">
      {props.rail && (
        <View
          pointerEvents="none"
          className="absolute top-[40px] bottom-[40px] left-[21px] items-center justify-evenly"
        >
          {[0, 1, 2, 3].map((dot) => (
            <View key={dot} className="size-[3px] rounded-full bg-foreground/30" />
          ))}
        </View>
      )}
      {props.children}
    </View>
    {props.footer}
  </View>
);

/** Turns the trip around: the arrows turn with it. */
export const SwapStops = (props: { readonly onSwap: () => void; }) => {
  const turn = useSharedValue(0);
  const turning = useAnimatedStyle(() => ({ transform: [{ rotate: `${turn.value}deg` }] }));

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel="Swap pickup and dropoff"
      onPress={() => {
        turn.value = withTiming(turn.value + 180, {
          duration: 300,
          easing: Easing.out(Easing.cubic),
        });
        props.onSwap();
      }}
      hitSlop={6}
      className="absolute top-1/2 right-2 -mt-[18px] size-9 items-center justify-center rounded-full bg-card active:bg-accent"
    >
      <Animated.View style={turning}>
        <Ionicons name="swap-vertical" size={17} color={colors.foreground} />
      </Animated.View>
    </Pressable>
  );
};

const MIN_QUERY = 3;

/**
 * From or To, edited in place: pressing the stop lifts its row into a white
 * field in the same position, the places found rise in beneath it, and
 * choosing one settles it back into the card. Which stop is being edited
 * belongs to the sheet, so the rest of it can step aside while the rider
 * searches.
 */
export const PlaceField = (props: {
  readonly field: Field;
  readonly label: string;
  readonly placeholder: string;
  readonly stop: Option.Option<Stop>;
  readonly editing: boolean;
  readonly onEdit: () => void;
  readonly onDone: () => void;
  readonly onChoose: (stop: Stop) => void;
}) => {
  const query = useAtomValue(placeQueryAtom(props.field));
  const setQuery = useAtomSet(placeQueryAtom(props.field));
  const results = useAtomValue(placeResultsAtom(props.field));

  const close = () => {
    setQuery("");
    props.onDone();
  };

  const choose = (place: Place) => {
    props.onChoose({ position: place.position, place: Option.some(place) });
    close();
  };

  if (!props.editing) {
    return (
      <Pressable
        accessibilityRole="button"
        accessibilityHint="Search for a place"
        onPress={() => {
          setQuery("");
          props.onEdit();
        }}
        className="flex-row items-center gap-3 rounded-xl py-2.5 pr-12 pl-3 active:bg-tile-hover"
      >
        <StopLine kind={props.field} label={props.label}>
          {Option.match(props.stop, {
            onNone: () => (
              <Text className="text-base font-medium text-muted-foreground">
                {props.placeholder}
              </Text>
            ),
            onSome: (stop) => <StopName stop={stop} />,
          })}
        </StopLine>
      </Pressable>
    );
  }

  const typed = query.trim();
  const offered = typed.length >= MIN_QUERY && AsyncResult.isSuccess(results)
      && Option.isSome(results.value)
    ? results.value.value
    : undefined;

  return (
    <View
      className="rounded-xl bg-card"
      style={{
        shadowColor: "#000000",
        shadowOpacity: 0.1,
        shadowRadius: 12,
        shadowOffset: { width: 0, height: 6 },
        elevation: 3,
      }}
    >
      <View className="flex-row items-center gap-3 py-2.5 pr-2.5 pl-3">
        <View className="w-5 items-center">
          <StopDot kind={props.field} />
        </View>
        <View className="min-w-0 flex-1">
          <Text className="text-xs font-semibold text-muted-foreground">{props.label}</Text>
          <TextInput
            autoFocus
            accessibilityLabel={props.label}
            value={query}
            onChangeText={setQuery}
            onBlur={close}
            placeholder={Option.isSome(props.stop)
              ? "Search for another place"
              : "Street, place or postcode"}
            placeholderTextColor={colors.mutedForeground}
            autoCorrect={false}
            autoCapitalize="none"
            returnKeyType="search"
            onSubmitEditing={() => {
              const first = offered?.[0];
              if (first !== undefined) choose(first);
            }}
            className="p-0 font-sans text-base font-semibold text-foreground"
          />
        </View>
        {query === ""
          ? (
            <Pressable
              accessibilityRole="button"
              onPress={close}
              hitSlop={8}
              className="rounded-full px-2.5 py-1.5 active:bg-tile"
            >
              <Text className="text-sm font-semibold">Cancel</Text>
            </Pressable>
          )
          : (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={`Clear ${props.label}`}
              onPress={() => {
                setQuery("");
              }}
              hitSlop={8}
              className="size-7 items-center justify-center rounded-full bg-tile active:bg-tile-hover"
            >
              <Ionicons name="close" size={15} color={colors.mutedForeground} />
            </Pressable>
          )}
      </View>

      <SearchState typed={typed} results={results} />
      {offered !== undefined && offered.length > 0 && (
        <View className="border-t border-border px-1.5 py-1.5">
          {offered.map((place, index) => (
            <Animated.View
              key={`${place.name}|${place.area}`}
              entering={FadeInDown.duration(200).delay(index * 25)}
            >
              <Pressable
                accessibilityRole="button"
                onPress={() => {
                  choose(place);
                }}
                className="flex-row items-center gap-3 rounded-lg px-2 py-2 active:bg-tile"
              >
                <View className="size-8 items-center justify-center rounded-full bg-tile">
                  <Ionicons name="location-outline" size={16} color={colors.mutedForeground} />
                </View>
                <View className="min-w-0 flex-1">
                  <Text numberOfLines={1} className="text-[15px] font-semibold">{place.name}</Text>
                  {place.area !== "" && (
                    <Text numberOfLines={1} className="text-[13px] text-muted-foreground">
                      {place.area}
                    </Text>
                  )}
                </View>
              </Pressable>
            </Animated.View>
          ))}
        </View>
      )}
    </View>
  );
};

/**
 * What the search is doing when it is not showing places. Too little typed
 * says nothing at all — the placeholder already asks for more.
 */
const SearchState = (props: {
  readonly typed: string;
  readonly results: AsyncResult.AsyncResult<Option.Option<ReadonlyArray<Place>>, unknown>;
}) => {
  if (props.typed.length < MIN_QUERY) return null;

  const line = (text: string) => (
    <View accessibilityLiveRegion="polite" className={cn("border-t border-border px-4 py-3")}>
      <Text className="text-sm text-muted-foreground">{text}</Text>
    </View>
  );

  if (AsyncResult.isFailure(props.results)) {
    return line("Address search is not answering. Tap the map, or pick a popular trip.");
  }
  if (!AsyncResult.isSuccess(props.results) || Option.isNone(props.results.value)) {
    return line("Searching…");
  }
  if (props.results.value.value.length === 0) {
    return line(`Nothing in Amsterdam matches “${props.typed}”.`);
  }
  return null;
};
