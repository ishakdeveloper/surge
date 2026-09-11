import { sessionAtom, signOut } from "@/atom/session-atoms.js";
import { Detail, Details } from "@/components/app/details.js";
import { ActionError } from "@/components/app/errors.js";
import { Screen } from "@/components/app/screen.js";
import { Sign, SignGlyph, SignIcon } from "@/components/sign/sign.js";
import { Button } from "@/components/ui/button.js";
import { Card } from "@/components/ui/card.js";
import { Heading, Text } from "@/components/ui/text.js";
import { useRestartSession } from "@/iam/session-scope.js";
import { serviceUrls } from "@/lib/config.js";
import { CardAction, CardActionError, CardSummary } from "@/ride/card-summary.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { contactOf, describeContact } from "@surge/domain/iam/Contact";
import { Exit } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { View } from "react-native";

/**
 * Who is signed in, as the Go services will see them.
 *
 * The decoded `Identity` is shown because it is the one thing the auth
 * arrangement has to get right — the same fields are what Go parses out of the
 * JWT — and the addresses because on a phone the usual reason nothing works is
 * that one of them is not reachable from the device.
 */
const Account = () => {
  const session = useAtomValue(sessionAtom);
  const leaving = useAtomValue(signOut);
  const leave = useAtomSet(signOut, { mode: "promiseExit" });
  const restart = useRestartSession();

  if (!AsyncResult.isSuccess(session)) return null;
  const identity = session.value;

  return (
    <Screen>
      <Details>
        <Detail label="Signed in" value={describeContact(contactOf(identity.email))} mono />
        <Detail label="Role" value={identity.role} mono />
      </Details>

      {identity.role === "ops" && (
        <Text className="text-muted-foreground">
          The dispatch console is on the web. This app is for riders and drivers.
        </Text>
      )}

      {identity.role === "rider" && (
        <View className="gap-2">
          <Heading>Payment</Heading>
          <Sign tone="service">
            <SignGlyph>
              <SignIcon name="card-outline" />
            </SignGlyph>
            <View className="min-w-0 flex-1">
              <CardSummary />
            </View>
            <CardAction />
          </Sign>
          <CardActionError />
        </View>
      )}

      <Card>
        <Heading>Connected to</Heading>
        <Details>
          <Detail label="API" value={serviceUrls.SURGE_API_URL} mono />
          <Detail label="Socket" value={serviceUrls.SURGE_WS_URL} mono />
          <Detail label="Auth" value={serviceUrls.AUTH_BASE_URL} mono />
        </Details>
      </Card>

      {AsyncResult.isFailure(leaving) && <ActionError cause={leaving.cause} />}
      <Button
        variant="outline"
        disabled={leaving.waiting}
        onPress={() => {
          void leave().then((exit) => {
            if (Exit.isSuccess(exit)) restart();
          });
        }}
      >
        {leaving.waiting ? "Signing out…" : "Sign out"}
      </Button>
    </Screen>
  );
};

export default Account;
