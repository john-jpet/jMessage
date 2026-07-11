import { useEffect, useState } from "react";

/** Minimal history-API routing for a two-view app: browser Back works,
 *  no router dependency. */
export function useRoute(): [string, (path: string) => void] {
  const [route, setRoute] = useState(location.pathname);

  useEffect(() => {
    const onPop = () => setRoute(location.pathname);
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  const navigate = (path: string) => {
    if (path !== location.pathname) {
      history.pushState({}, "", path);
    }
    setRoute(path);
  };
  return [route, navigate];
}
