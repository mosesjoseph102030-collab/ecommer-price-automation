"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type CommunityRoom, type CommunityPost, type CommunityReply, type Announcement } from "@/lib/api";

export default function CommunityManager({ slug }: { slug: string }) {
  const [rooms, setRooms] = useState<CommunityRoom[]>([]);
  const [room, setRoom] = useState<CommunityRoom | null>(null);
  const [posts, setPosts] = useState<CommunityPost[]>([]);
  const [replies, setReplies] = useState<Record<string, CommunityReply[]>>({});
  const [openPost, setOpenPost] = useState<string | null>(null);
  const [announcements, setAnnouncements] = useState<Announcement[]>([]);
  const [body, setBody] = useState("");
  const [reply, setReply] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [working, setWorking] = useState(false);

  const load = useCallback(async () => {
    try {
      const [roomList, inbox] = await Promise.all([api.communityRooms(), api.announcements(slug)]);
      setRooms(roomList.rooms);
      setAnnouncements(inbox.announcements);
      setRoom((current) => current ?? roomList.rooms.find((r) => r.can_post) ?? roomList.rooms[0] ?? null);
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not load community.");
    }
  }, [slug]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!room) return;
    api
      .communityPosts(room.id)
      .then((r) => setPosts(r.posts))
      .catch(() => setPosts([]));
  }, [room]);

  async function run(action: () => Promise<unknown>, success: string) {
    setWorking(true);
    setError("");
    setMessage("");
    try {
      await action();
      setMessage(success);
      await load();
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Action failed.");
    } finally {
      setWorking(false);
    }
  }

  async function post() {
    if (!room || body.trim().length === 0) return;
    setWorking(true);
    setError("");
    try {
      await api.createCommunityPost(room.id, body);
      setBody("");
      setMessage("Posted.");
      const refreshed = await api.communityPosts(room.id);
      setPosts(refreshed.posts);
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not post.");
    } finally {
      setWorking(false);
    }
  }

  async function replyTo(postId: string) {
    if (reply.trim().length === 0) return;
    setWorking(true);
    try {
      await api.createCommunityReply(postId, reply);
      setReply("");
      const refreshed = await api.communityReplies(postId);
      setReplies((current) => ({ ...current, [postId]: refreshed.replies }));
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not reply.");
    } finally {
      setWorking(false);
    }
  }

  return (
    <div className="stack">
      <section className="panel">
        <h2>Owner community</h2>
        <p className="muted">
          Verified store owners only. General strategy is welcome. Coordinated pricing, customer allocation, sharing
          competitor agreements, and price fixing across sellers are blocked.
        </p>
      </section>

      {error && <div className="alert alert-error">{error}</div>}
      {message && <div className="alert alert-success">{message}</div>}

      {announcements.length > 0 && (
        <section className="panel">
          <h3>Announcements</h3>
          {announcements.map((a) => (
            <div key={a.id} className={a.severity === "critical" ? "alert alert-error" : "alert"}>
              <strong>{a.title}</strong>
              <div>{a.body_text}</div>
              <div className="row">
                <span className="muted">v{a.current_version}</span>
                {!a.acknowledged_at && (
                  <button className="btn" onClick={() => void run(() => api.markAnnouncementRead(slug, a.id, true), "Acknowledged.")}>
                    Acknowledge
                  </button>
                )}
              </div>
            </div>
          ))}
        </section>
      )}

      <div className="row">
        {rooms.map((r) => (
          <button key={r.id} className={room?.id === r.id ? "btn btn-primary" : "btn"} onClick={() => setRoom(r)}>
            {r.name}
            {r.kind === "announcement" ? " (read-only)" : ""}
          </button>
        ))}
      </div>

      {room?.can_post && (
        <section className="panel stack">
          <h3>Start a discussion in {room.name}</h3>
          <div className="field">
            <label htmlFor="cm-body">Message</label>
            <textarea id="cm-body" rows={4} value={body} onChange={(e) => setBody(e.target.value)} maxLength={5000} />
          </div>
          <button className="btn btn-primary" disabled={working || body.trim().length === 0} onClick={() => void post()}>
            Post
          </button>
          <p className="muted">
            Please do not post your own cost, supplier, or revenue figures. Commercial figures are redacted from
            moderation previews.
          </p>
        </section>
      )}

      <section className="panel table-wrap">
        <h3>{room?.name ?? "Rooms"}</h3>
        <table>
          <thead>
            <tr>
              <th>Author</th>
              <th>Message</th>
              <th>Replies</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {posts.length === 0 && (
              <tr>
                <td colSpan={4} className="muted">
                  No posts yet.
                </td>
              </tr>
            )}
            {posts.map((p) => (
              <tr key={p.id}>
                <td>
                  <strong>{p.author_name || "Owner"}</strong>
                  <div className="muted">{new Date(p.created_at).toLocaleDateString()}</div>
                </td>
                <td>{p.body}</td>
                <td>{p.reply_count}</td>
                <td>
                  <div className="row">
                    <button
                      className="btn"
                      onClick={async () => {
                        setOpenPost(openPost === p.id ? null : p.id);
                        if (openPost !== p.id) {
                          const r = await api.communityReplies(p.id);
                          setReplies((current) => ({ ...current, [p.id]: r.replies }));
                        }
                      }}
                    >
                      {openPost === p.id ? "Hide" : "Replies"}
                    </button>
                    <button
                      className="btn"
                      onClick={() => {
                        const reason = window.prompt("Why are you reporting this post?");
                        if (reason) void run(() => api.reportCommunityContent("post", p.id, reason), "Reported for moderation.");
                      }}
                    >
                      Report
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      {openPost && (
        <section className="panel stack">
          <h3>Replies</h3>
          {(replies[openPost] ?? []).map((r) => (
            <div key={r.id}>
              <strong>{r.author_name || "Owner"}</strong>
              <div>{r.body}</div>
            </div>
          ))}
          <div className="field">
            <label htmlFor="cm-reply">Your reply</label>
            <textarea id="cm-reply" rows={3} value={reply} onChange={(e) => setReply(e.target.value)} />
          </div>
          <button className="btn btn-primary" disabled={working || reply.trim().length === 0} onClick={() => void replyTo(openPost)}>
            Reply
          </button>
        </section>
      )}
    </div>
  );
}
