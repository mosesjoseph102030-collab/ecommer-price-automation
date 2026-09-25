"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type ReportData } from "@/lib/api";

const KIND_LABELS: Record<string, string> = {
  margin: "Product margin",
  below_floor: "Below floor / target margin",
  competitor_gap: "Competitor gap",
  price_change_history: "Price-change history",
  publish_reliability: "Approval & publish reliability",
  competitor_health: "Competitor monitoring health",
  recommendation_outcomes: "Recommendation outcomes",
  stock_opportunity: "Stock-aware opportunities",
};

export default function ReportManager({ slug }: { slug: string }) {
  const [kinds, setKinds] = useState<string[]>([]);
  const [kind, setKind] = useState("margin");
  const [report, setReport] = useState<ReportData | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    api
      .reportKinds(slug)
      .then((r) => {
        setKinds(r.kinds);
        if (!r.kinds.includes(kind)) setKind(r.kinds[0] ?? "margin");
      })
      .catch((cause) => setError(cause instanceof ApiError ? cause.message : "Could not load report list."));
  }, [slug, kind]);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      setReport(await api.report(slug, kind));
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Could not load the report.");
    } finally {
      setLoading(false);
    }
  }, [slug, kind]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <div className="stack">
      <section className="panel">
        <div className="row between">
          <div>
            <h2>Reports</h2>
            <p className="muted">Every report is scoped to your store and respects role permissions.</p>
          </div>
          <a className="btn" href={api.reportExportUrl(slug, kind)}>
            Export CSV
          </a>
        </div>
        <div className="field">
          <label htmlFor="report-kind">Report</label>
          <select id="report-kind" value={kind} onChange={(e) => setKind(e.target.value)}>
            {kinds.map((k) => (
              <option key={k} value={k}>
                {KIND_LABELS[k] ?? k}
              </option>
            ))}
          </select>
        </div>
      </section>

      {error && <div className="alert alert-error">{error}</div>}
      {loading && <div className="panel muted">Loading report…</div>}

      {report && !loading && (
        <section className="panel table-wrap">
          <h3>
            {KIND_LABELS[report.kind] ?? report.kind} ({report.row_count} rows)
          </h3>
          {report.rows.length === 0 ? (
            <p className="muted">No data for this report yet.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  {report.columns.map((column) => (
                    <th key={column}>{column.replace(/_/g, " ")}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {report.rows.map((row, index) => (
                  <tr key={index}>
                    {row.map((cell, cellIndex) => (
                      <td key={cellIndex}>{cell}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      )}
    </div>
  );
}
