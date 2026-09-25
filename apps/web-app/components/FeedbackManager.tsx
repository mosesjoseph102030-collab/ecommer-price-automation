"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api, ApiError, type FeedbackTicket } from "@/lib/api";

export default function FeedbackManager({ slug }: { slug: string }) {
  const [tickets, setTickets] = useState<FeedbackTicket[]>([]);
  const [selected, setSelected] = useState<FeedbackTicket | null>(null);
  const [reply, setReply] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [working, setWorking] = useState(false);

  const load = useCallback(async () => {
    try {
      const list = await api.feedbackTickets(slug);
      setTickets(list.tickets);
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not load feedback.");
    }
  }, [slug]);

  useEffect(() => {
    void load();
  }, [load]);

  async function open(id: string) {
    try {
      setSelected(await api.feedbackTicket(slug, id));
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not open the ticket.");
    }
  }

  // FormEvent<HTMLFormElement> rather than bare FormEvent: currentTarget is
  // EventTarget & Element without the element type parameter, which FormData
  // and form.reset() reject.
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setWorking(true);
    setError("");
    setMessage("");
    try {
      await api.createFeedbackTicket(slug, {
        kind: String(form.get("kind")),
        subject: String(form.get("subject")),
        body: String(form.get("body")),
      });
      setMessage("Feedback sent. Our team replies in this private thread.");
      await load();
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not send feedback.");
    } finally {
      setWorking(false);
    }
  }

  async function sendReply() {
    if (!selected || reply.trim().length === 0) return;
    setWorking(true);
    try {
      await api.replyToFeedback(slug, selected.id, reply);
      setReply("");
      setMessage("Reply added.");
      await open(selected.id);
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not send reply.");
    } finally {
      setWorking(false);
    }
  }

  return (
    <div className="stack">
      <section className="panel">
        <h2>Private feedback</h2>
        <p className="muted">
          Only you and the support team can see this thread. We never share your store data when replying.
        </p>
      </section>

      {error && <div className="alert alert-error">{error}</div>}
      {message && <div className="alert alert-success">{message}</div>}

      <div className="grid pricing-grid">
        <form className="panel stack" onSubmit={submit}>
          <h3>New feedback</h3>
          <div className="field">
            <label htmlFor="fb-kind">Type</label>
            <select id="fb-kind" name="kind" defaultValue="bug">
              <option value="bug">Bug report</option>
              <option value="feature_request">Feature request</option>
              <option value="improvement">Improvement suggestion</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="fb-subject">Subject</label>
            <input id="fb-subject" name="subject" required maxLength={200} />
          </div>
          <div className="field">
            <label htmlFor="fb-body">What happened?</label>
            <textarea id="fb-body" name="body" rows={6} required maxLength={10000} />
          </div>
          <button type="submit" className="btn btn-primary" disabled={working}>
            Send feedback
          </button>
        </form>

        <section className="panel table-wrap">
          <h3>Your tickets</h3>
          <table>
            <thead>
              <tr>
                <th>Subject</th>
                <th>Type</th>
                <th>Status</th>
                <th>Updated</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {tickets.length === 0 && (
                <tr>
                  <td colSpan={5} className="muted">
                    No feedback yet.
                  </td>
                </tr>
              )}
              {tickets.map((ticket) => (
                <tr key={ticket.id}>
                  <td>
                    <strong>{ticket.subject}</strong>
                    {ticket.duplicate_of && <div className="muted">merged into another ticket</div>}
                  </td>
                  <td className="muted">{ticket.kind.replace(/_/g, " ")}</td>
                  <td>
                    <span className="badge">{ticket.status.replace(/_/g, " ")}</span>
                  </td>
                  <td className="muted">{new Date(ticket.updated_at).toLocaleDateString()}</td>
                  <td>
                    <button className="btn" onClick={() => void open(ticket.id)}>
                      Open
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      </div>

      {selected && (
        <section className="panel">
          <div className="row between">
            <h3>{selected.subject}</h3>
            <button className="btn" onClick={() => setSelected(null)}>
              Close
            </button>
          </div>
          <p className="muted">
            Status: {selected.status.replace(/_/g, " ")} · Priority: {selected.priority}
          </p>
          <div className="stack">
            {(selected.messages ?? []).map((m) => (
              <div key={m.id} className={m.author_kind === "support" ? "alert" : ""}>
                <strong>{m.author_kind === "support" ? "Support" : "You"}</strong>
                <div>{m.body}</div>
                <div className="muted">{new Date(m.created_at).toLocaleString()}</div>
              </div>
            ))}
          </div>
          <div className="field">
            <label htmlFor="fb-reply">Reply</label>
            <textarea id="fb-reply" rows={4} value={reply} onChange={(e) => setReply(e.target.value)} />
          </div>
          <button className="btn btn-primary" disabled={working || reply.trim().length === 0} onClick={() => void sendReply()}>
            Send reply
          </button>
        </section>
      )}
    </div>
  );
}
