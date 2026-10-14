import { useState, type FormEvent } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router";

import { ApiError } from "@/shared/api-client";

import { useLogin } from "../../hooks/use-login";
import { useMe } from "../../hooks/use-me";

import { loginStyles as styles } from "./login.styles";

/** Only same-app paths are accepted as redirect targets (no open redirect). */
function safeNext(next: string | null): string {
  return next && next.startsWith("/") && !next.startsWith("//") ? next : "/";
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401) return "Invalid username or password.";
    if (err.status === 429) return "Too many attempts. Wait a few minutes and try again.";
    return err.message;
  }
  return "Cannot reach the SEMAR API.";
}

export function LoginPage() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { data: me } = useMe();
  const login = useLogin();
  const next = safeNext(params.get("next"));

  if (me) return <Navigate to={next} replace />;

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate({ username, password }, { onSuccess: () => navigate(next, { replace: true }) });
  };

  return (
    <main className={styles.page}>
      <section className={styles.card} aria-labelledby="login-title">
        <header className={styles.header}>
          <h1 id="login-title" className={styles.title}>
            Sign in to SEMAR
          </h1>
          <p className={styles.subtitle}>Unified cloud control plane</p>
        </header>
        <form className={styles.form} onSubmit={onSubmit}>
          {login.isError && (
            <p className={styles.error} role="alert">
              {errorMessage(login.error)}
            </p>
          )}
          <label className={styles.field}>
            <span className={styles.label}>Username</span>
            <input
              className={styles.input}
              name="username"
              autoComplete="username"
              required
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </label>
          <label className={styles.field}>
            <span className={styles.label}>Password</span>
            <input
              className={styles.input}
              name="password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </label>
          <button type="submit" className={styles.submit} disabled={login.isPending}>
            {login.isPending ? "Signing in…" : "Sign in"}
          </button>
        </form>
      </section>
    </main>
  );
}
