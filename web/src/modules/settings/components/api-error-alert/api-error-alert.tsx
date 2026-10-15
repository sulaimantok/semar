import { ApiError } from "@/shared/api-client";

import { apiErrorAlertStyles as styles } from "./api-error-alert.styles";

function message(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 403) return "You do not have permission to do this.";
    if (err.status === 409) return "An entry with this identity already exists.";
    return err.message;
  }
  return "Cannot reach the SEMAR API.";
}

/** Shows an API failure with its request ID for support. */
export function ApiErrorAlert({ error }: { error: unknown }) {
  if (!error) return null;
  const requestId = error instanceof ApiError ? error.requestId : undefined;
  return (
    <p className={styles.root} role="alert">
      {message(error)}
      {requestId && <span className={styles.requestId}>Request ID: {requestId}</span>}
    </p>
  );
}
