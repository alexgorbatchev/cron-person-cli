import { useState } from 'react';
import type { ChangeEvent } from 'react';
import { Button, Input, Label } from '@alexgorbatchev/shadcn';

interface TaskEditorProps {
  content: string;
  onChange: (content: string) => void;
  disabled: boolean;
}
export function TaskEditor({ content, onChange, disabled }: TaskEditorProps) {
  const [schedule, setSchedule] = useState('0 9 * * *');
  const [command, setCommand] = useState('');
  const [editing, setEditing] = useState<number | null>(null);
  const lines = content.split('\n');
  const tasks = lines.flatMap((line, index) => {
    const match = line.trim().match(/^(@\S+|\S+\s+\S+\s+\S+\s+\S+\s+\S+)\s+(.+)$/);
    if (!match || /^[A-Za-z_][A-Za-z0-9_]*\s*=/.test(line.trim()) || line.trim().startsWith('#')) return [];
    return [{ index, schedule: match[1] ?? '', command: match[2] ?? '' }];
  });
  return (
    <section className="space-y-4">
      <h2 className="font-semibold">Tasks ({tasks.length})</h2>
      {tasks.length ? (
        <div className="overflow-x-auto">
          <table className="w-full text-sm text-left">
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th className="p-2">Schedule</th>
                <th className="p-2">Command</th>
                <th className="p-2">Actions</th>
              </tr>
            </thead>
            <tbody>
              {tasks.map((task) => (
                <tr key={task.index} className="border-b border-border">
                  <td className="p-2 whitespace-nowrap">{task.schedule}</td>
                  <td className="p-2 break-all">{task.command}</td>
                  <td className="p-2">
                    <div className="flex gap-2">
                      <Button
                        variant="outline"
                        disabled={disabled}
                        onClick={() => {
                          setEditing(task.index);
                          setSchedule(task.schedule);
                          setCommand(task.command);
                        }}
                      >
                        Edit
                      </Button>
                      <Button
                        variant="outline"
                        disabled={disabled}
                        onClick={() => {
                          onChange(lines.filter((_, index) => index !== task.index).join('\n'));
                          setEditing(null);
                        }}
                      >
                        Remove
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">No tasks yet. Add a schedule and a shell command.</p>
      )}
      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          const line = `${schedule.trim()} ${command.trim()}`;
          if (editing === null) onChange(`${content.trimEnd()}${content.trimEnd() ? '\n' : ''}${line}\n`);
          else onChange(lines.map((value, index) => (index === editing ? line : value)).join('\n'));
          setEditing(null);
          setCommand('');
        }}
      >
        <div className="space-y-2">
          <Label htmlFor="schedule">Schedule</Label>
          <Input
            id="schedule"
            required
            value={schedule}
            onChange={(event: ChangeEvent<HTMLInputElement>) => setSchedule(event.target.value)}
          />
        </div>
        <div className="space-y-2 grow">
          <Label htmlFor="command">Shell command</Label>
          <Input
            id="command"
            required
            value={command}
            onChange={(event: ChangeEvent<HTMLInputElement>) => setCommand(event.target.value)}
          />
        </div>
        <Button type="submit" disabled={disabled}>
          {editing === null ? 'Add task' : 'Update task'}
        </Button>
        {editing !== null ? (
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setEditing(null);
              setCommand('');
            }}
          >
            Cancel
          </Button>
        ) : null}
      </form>
      <p className="text-xs text-muted-foreground">Task changes are drafts until you save the configuration below.</p>
    </section>
  );
}
