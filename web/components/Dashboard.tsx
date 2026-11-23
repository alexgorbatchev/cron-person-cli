import type { ChangeEvent } from 'react';
import { useEffect, useState } from 'react';
import { Button, Input, Label, Textarea } from '@alexgorbatchev/shadcn';

import { TaskEditor } from './TaskEditor';

interface Directory {
  dir: string;
}
interface Document {
  dir: string;
  content: string;
  revision: string;
  status: string;
}
const token = window.location.hash.slice(1);
async function request(path: string, method = 'GET', body?: Document) {
  const response = await fetch(`/api/${path}`, {
    method,
    headers: { 'X-Dashboard-Token': token, 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}
export function Dashboard() {
  const [directories, setDirectories] = useState<Directory[]>([]);
  const [path, setPath] = useState('');
  const [doc, setDoc] = useState<Document | null>(null);
  const [content, setContent] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  async function refresh() {
    setDirectories((await request('directories')) ?? []);
  }
  async function load(dir: string) {
    const next: Document = await request(`document?dir=${encodeURIComponent(dir)}`);
    setDoc(next);
    setContent(next.content);
    setPath(dir);
  }
  async function action(work: () => Promise<void>) {
    setBusy(true);
    setMessage('');
    try {
      await work();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Request failed');
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    void action(refresh);
  }, []);
  return (
    <main className="min-h-screen bg-background text-foreground font-mono p-6 md:p-10">
      <header className="border-b border-border pb-6 mb-8 flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="text-xs text-muted-foreground mb-2">LOCAL SCHEDULER</p>
          <h1 className="text-2xl font-semibold">cron-person</h1>
        </div>
        <span className="text-sm text-muted-foreground">Dashboard / Configurations</span>
      </header>
      <div className="grid gap-8 md:grid-cols-[260px_minmax(0,1fr)]">
        <aside className="space-y-4 min-w-0">
          <h2 className="font-semibold">
            Tracked directories <span className="text-muted-foreground">({directories.length})</span>
          </h2>
          <div className="flex flex-col gap-2">
            {directories.map((dir) => (
              <Button
                key={dir.dir}
                variant="outline"
                disabled={busy}
                onClick={() => {
                  if (!doc || content === doc.content || window.confirm('Discard unsaved changes?'))
                    void action(() => load(dir.dir));
                }}
              >
                <span className="truncate" title={dir.dir}>
                  {dir.dir.split('/').filter(Boolean).pop() || dir.dir}
                </span>
              </Button>
            ))}
          </div>
          <p className="text-sm text-muted-foreground">
            Open an existing directory below to create or edit its configuration.
          </p>
        </aside>
        <section className="space-y-6 min-w-0">
          <form
            className="space-y-3"
            onSubmit={(event) => {
              event.preventDefault();
              if (!doc || content === doc.content || window.confirm('Discard unsaved changes?'))
                void action(() => load(path));
            }}
          >
            <Label htmlFor="directory">Directory</Label>
            <div className="flex gap-2">
              <Input
                id="directory"
                value={path}
                onChange={(event: ChangeEvent<HTMLInputElement>) => setPath(event.target.value)}
                placeholder="/absolute/path/to/project"
                required
              />
              <Button type="submit" disabled={busy}>
                Open
              </Button>
            </div>
          </form>
          {doc ? (
            <>
              <div className="flex justify-between gap-3 flex-wrap">
                <h2 className="font-semibold">.cronrc</h2>
                <span className="text-sm text-muted-foreground">
                  {doc.status}
                  {content !== doc.content ? ' / unsaved changes' : ''}
                </span>
              </div>
              <p className="text-sm text-muted-foreground">
                Create, edit, or remove task lines below. Environment assignments and comments are preserved. Saving
                requires a separate approval before execution.
              </p>
              <TaskEditor key={doc.revision} content={content} onChange={setContent} disabled={busy} />
              <Label htmlFor="content">Configuration source</Label>
              <Textarea
                id="content"
                rows={16}
                value={content}
                onChange={(event) => setContent(event.target.value)}
                placeholder={'# A task per line\n0 9 * * * echo hello'}
              />
              <div className="flex gap-3 flex-wrap">
                <Button
                  disabled={busy}
                  onClick={() =>
                    void action(async () => {
                      await request('document', 'PUT', { ...doc, content });
                      await load(doc.dir);
                      await refresh();
                      setMessage('Saved. Use Allow to approve this configuration.');
                    })
                  }
                >
                  Save configuration
                </Button>
                <Button
                  variant="outline"
                  disabled={busy || content !== doc.content}
                  onClick={() =>
                    void action(async () => {
                      await request('document', 'POST', { ...doc, status: 'allowed' });
                      await load(doc.dir);
                      await refresh();
                    })
                  }
                >
                  Allow
                </Button>
                <Button
                  variant="outline"
                  disabled={busy}
                  onClick={() =>
                    void action(async () => {
                      await request('document', 'POST', { ...doc, status: 'denied' });
                      await load(doc.dir);
                      await refresh();
                    })
                  }
                >
                  Deny
                </Button>
                <Button
                  variant="destructive"
                  disabled={busy}
                  onClick={() => {
                    if (window.confirm(`Delete ${doc.dir}/.cronrc and revoke its authorization?`))
                      void action(async () => {
                        await request('document', 'DELETE', doc);
                        setDoc(null);
                        await refresh();
                        setMessage('Configuration deleted.');
                      });
                  }}
                >
                  Delete configuration
                </Button>
              </div>
            </>
          ) : (
            <div className="border border-dashed border-border p-10 space-y-3">
              <h2 className="text-lg">Your schedules, in one place.</h2>
              <p className="text-muted-foreground text-sm">
                Select a tracked directory or open an absolute path to manage its .cronrc tasks.
              </p>
            </div>
          )}
          <p role="status" className="text-sm whitespace-pre-wrap">
            {busy ? 'Working…' : message}
          </p>
        </section>
      </div>
    </main>
  );
}
