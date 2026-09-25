"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";

const PUBLIC_URL = process.env.NEXT_PUBLIC_PUBLIC_URL ?? "http://localhost:3000";

type State =
  | { phase: "loading" }
  | { phase: "anonymous" }
  | { phase: "empty"; email: string }
  | { phase: "picker"; email: string; stores: Array<{ id: string; name: string; slug: string }> };

// Every dashboard route is tenant-scoped (/app/{slug}/...), so the bare origin has
// no store to render. Rather than a dead end, this resolves the session and the
// caller's stores and routes them to a real page: straight to the single store
// they have, to a picker when they have several, and to onboarding when they
// have none.
export default function DashboardEntry() {
  const [state, setState] = useState<State>({ phase: "loading" });
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  const resolve = useCallback(async () => {
    setState({ phase: "loading" });
    setError("");

    let email: string;
    try {
      const user = await api.me();
      email = user.email;
    } catch (cause) {
      // 401 is the expected answer for an anonymous visitor, not a failure worth
      // reporting. Anything else is a real problem the user needs to see.
      if (!(cause instanceof ApiError) || cause.status !== 401) {
        setError(cause instanceof ApiError ? cause.message : "Could not reach the API.");
      }
      setState({ phase: "anonymous" });
      return;
    }

    try {
      const { organizations } = await api.organizations();
      if (organizations.length === 0) {
        setState({ phase: "empty", email });
        return;
      }
      if (organizations.length === 1) {
        // One store is not a choice, so do not make the user make it.
        window.location.replace(`/app/${encodeURIComponent(organizations[0].slug)}/overview`);
        return;
      }
      setState({ phase: "picker", email, stores: organizations });
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not load your stores.");
      setState({ phase: "anonymous" });
    }
  }, []);

  useEffect(() => {
    void resolve();
  }, [resolve]);

  async function createStore(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setCreating(true);
    setError("");
    try {
      const store = await api.createOrganization({
        name: String(form.get("name") ?? ""),
        currency: "NGN",
        timezone: "Africa/Lagos",
      });
      window.location.assign(`/app/${encodeURIComponent(store.slug)}/settings/woocommerce`);
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not create the store.");
      setCreating(false);
    }
  }

  if (state.phase === "loading") {
    return (
      <main className="app-main">
        <h1>Store dashboard</h1>
        <p className="muted">Checking your session…</p>
      </main>
    );
  }

  if (state.phase === "picker") {
    return (
      <main className="app-main">
        <header className="page-header">
          <div>
            <p className="muted">Signed in as {state.email}</p>
            <h1>Choose a store</h1>
            <p className="muted">You have access to {state.stores.length} stores.</p>
          </div>
        </header>
        {error && <div className="alert alert-error">{error}</div>}
        <div className="grid">
          {state.stores.map((store) => (
            <a key={store.id} className="panel" href={`/app/${encodeURIComponent(store.slug)}/overview`}>
              <h3>{store.name}</h3>
              <p className="muted">
                /app/{store.slug}
              </p>
            </a>
          ))}
        </div>
      </main>
    );
  }

  if (state.phase === "empty") {
    return (
      <main className="app-main">
        <header className="page-header">
          <div>
            <p className="muted">Signed in as {state.email}</p>
            <h1>Create your first store</h1>
            <p className="muted">
              A store holds one WooCommerce catalog. You can add competitors, costs, and pricing rules once it exists.
            </p>
          </div>
        </header>
        {error && <div className="alert alert-error">{error}</div>}
        <form className="panel stack" onSubmit={createStore}>
          <div className="field">
            <label htmlFor="store-name">Store name</label>
            <input id="store-name" name="name" required placeholder="e.g. Ada&apos;s Fashion" />
          </div>
          <div className="muted">A URL like /app/your-store is generated from the name.</div>
          <button className="btn btn-primary" type="submit" disabled={creating}>
            {creating ? "Creating…" : "Create store"}
          </button>
        </form>
      </main>
    );
  }

  return (
    <main className="app-main">
      <header className="page-header">
        <div>
          <h1>Store dashboard</h1>
          <p className="muted">Sign in to open your store dashboard.</p>
        </div>
      </header>
      {error && <div className="alert alert-error">{error}</div>}
      <div className="panel stack">
        <p>
          This dashboard is tenant-scoped: every page belongs to one store, so the address always includes the store
          name, like <code>/app/your-store/overview</code>.
        </p>
        <p>Once you sign in you will land straight on your store, or pick one if you have several.</p>
        <div>
          <a className="btn btn-primary" href={`${PUBLIC_URL}/signin`}>
            Sign in
          </a>
        </div>
        <p className="muted">
          No account yet? <a href={`${PUBLIC_URL}/signup`}>Create one</a>.
        </p>
      </div>
    </main>
  );
}
