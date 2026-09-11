import * as React from "react";
import { ScrollView } from "react-native";

/**
 * One scrolling column, the phone's version of the web's `SplitView` aside.
 *
 * No safe-area handling: every screen sits under a navigator header and above a
 * tab bar, and both already inset themselves.
 */
export const Screen = (props: { readonly children: React.ReactNode; }) => (
  <ScrollView
    className="flex-1 bg-background"
    contentContainerClassName="gap-6 p-4 pb-10"
    keyboardShouldPersistTaps="handled"
  >
    {props.children}
  </ScrollView>
);
