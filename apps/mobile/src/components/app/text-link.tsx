import { Text } from "@/components/ui/text.js";
import { type Href, Link } from "expo-router";

/**
 * Navigation is a link, never a button — the repo rule, and on a phone it is
 * what VoiceOver and TalkBack announce as one.
 */
export const TextLink = (props: { readonly href: Href; readonly children: string; }) => (
  <Link href={props.href} asChild>
    <Text accessibilityRole="link" className="text-primary underline">
      {props.children}
    </Text>
  </Link>
);
