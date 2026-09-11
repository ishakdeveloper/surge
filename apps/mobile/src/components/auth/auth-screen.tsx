import { Screen } from "@/components/app/screen.js";
import { Text } from "@/components/ui/text.js";
import * as React from "react";
import { KeyboardAvoidingView, Platform, View } from "react-native";

/** Shared chrome for the auth screens: a sentence, the form, and the ways out. */
export const AuthScreen = (props: {
  readonly description: string;
  readonly children: React.ReactNode;
  readonly footer: React.ReactNode;
}) => (
  <KeyboardAvoidingView
    behavior={Platform.OS === "ios" ? "padding" : "height"}
    className="flex-1 bg-background"
  >
    <Screen>
      <Text className="text-muted-foreground">{props.description}</Text>
      {props.children}
      <View className="gap-2">{props.footer}</View>
    </Screen>
  </KeyboardAvoidingView>
);
