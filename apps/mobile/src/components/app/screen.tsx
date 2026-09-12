import { useTabBarSpace } from "@/components/app/tab-bar.js";
import { Text } from "@/components/ui/text.js";
import * as React from "react";
import { ScrollView } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

/**
 * One scrolling column, the phone's version of the web's page card.
 *
 * With a `title` it is a tab's screen: no navigator header, so it clears the
 * status bar itself and opens with the title large and bold, the way a ride
 * app's tabs do. Inside the tabs it leaves room at the bottom for the floating
 * bar.
 */
export const Screen = (props: { readonly title?: string; readonly children: React.ReactNode; }) => {
  const insets = useSafeAreaInsets();
  const bar = useTabBarSpace();

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="gap-6 px-4"
      contentContainerStyle={{
        paddingTop: props.title === undefined ? 16 : insets.top + 12,
        paddingBottom: bar + 28,
      }}
      keyboardShouldPersistTaps="handled"
    >
      {props.title !== undefined && (
        <Text accessibilityRole="header" className="text-2xl leading-[30px] font-semibold">
          {props.title}
        </Text>
      )}
      {props.children}
    </ScrollView>
  );
};
