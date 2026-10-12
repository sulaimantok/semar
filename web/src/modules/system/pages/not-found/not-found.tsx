import { Link, useLocation } from "react-router";

import { notFoundStyles as styles } from "./not-found.styles";

export function NotFoundPage() {
  const { pathname } = useLocation();
  return (
    <div className={styles.page}>
      <h1 className={styles.title}>Page not found</h1>
      <p className={styles.text}>
        Nothing lives at <code>{pathname}</code> yet.
      </p>
      <Link to="/" className={styles.link}>
        Back to overview
      </Link>
    </div>
  );
}
