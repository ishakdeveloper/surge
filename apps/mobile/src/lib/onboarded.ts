import * as SecureStore from "expo-secure-store";

/**
 * Whether an account has finished or skipped onboarding on this phone. A
 * convenience, not state anyone else needs: an unreadable store just means the
 * flow shows again, which it can always be skipped from.
 */
const key = (userId: string) => `surge.onboarded.${userId.replace(/[^\w.-]/g, "_")}`;

export const hasOnboarded = (userId: string): boolean => {
  try {
    return SecureStore.getItem(key(userId)) === "1";
  } catch {
    return false;
  }
};

export const rememberOnboarded = (userId: string) => {
  try {
    SecureStore.setItem(key(userId), "1");
  } catch {
    // A locked Keychain: it shows again next time, and can be skipped.
  }
};
