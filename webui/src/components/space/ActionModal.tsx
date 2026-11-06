import { useState } from 'react';
import { createPortal } from 'react-dom';
import { useQueryClient } from '@tanstack/react-query';
import { api, queryKeys } from '../../api';
import type { Graph, GraphNode } from '../../types';
import { isSpaceNode } from '../../utils';
import styles from './ActionModal.module.css';

export type SpaceActionName = 'delete' | 'archive' | 'transfer';

const WORDS: Record<SpaceActionName, string> = {
  delete: 'DELETE',
  archive: 'ARCHIVE',
  transfer: 'TRANSFER',
};

interface ActionModalProps {
  node: GraphNode;
  graph?: Graph;
  action: SpaceActionName;
  onClose: () => void;
}

export default function ActionModal({ node, graph, action, onClose }: ActionModalProps) {
  const queryClient = useQueryClient();
  const [phrase, setPhrase] = useState('');
  const [account, setAccount] = useState('');
  const [server, setServer] = useState('');
  const [error, setError] = useState('');
  const [note, setNote] = useState('');
  const [pending, setPending] = useState(false);
  const word = WORDS[action];
  const typed = phrase.trim();
  const accounts = (graph?.nodes ?? []).filter(n => n.type === 'github_account' && isSpaceNode(n));
  const ready = typed === word && (action !== 'transfer' || account.trim() !== '' || server.trim() !== '');

  const submit = async () => {
    setError('');
    setPending(true);
    try {
      const res = await api.postSpaceAction({
        node_id: node.id,
        action,
        confirm: typed,
        target_account: account.trim() || undefined,
        target_server: server.trim() || undefined,
      });
      setNote(res.data.note || 'Recorded. The host was not modified.');
      await queryClient.invalidateQueries({ queryKey: queryKeys.graph });
      await queryClient.invalidateQueries({ queryKey: ['space-summary'] });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Action rejected');
    } finally {
      setPending(false);
    }
  };

  return createPortal(
    <div className={styles.backdrop} role="presentation" onMouseDown={onClose}>
      <div
        className={styles.modal}
        role="dialog"
        aria-modal="true"
        aria-labelledby="space-action-title"
        onMouseDown={e => e.stopPropagation()}
      >
        <h2 id="space-action-title">{word} {node.name}</h2>
        <p className={styles.warn}>
          This records the request on the map. GoXIO does not delete, archive, or move anything on the host.
        </p>
        {action === 'transfer' && (
          <div className={styles.fields}>
            <label>
              GitHub account
              <input
                list="goxio-accounts"
                value={account}
                onChange={e => setAccount(e.target.value)}
                placeholder="login"
                aria-label="Target GitHub account"
              />
              <datalist id="goxio-accounts">
                {accounts.map(a => <option key={a.id} value={a.name} />)}
              </datalist>
            </label>
            <label>
              Server
              <input
                value={server}
                onChange={e => setServer(e.target.value)}
                placeholder="server name"
                aria-label="Target server"
              />
            </label>
          </div>
        )}
        <label className={styles.confirm}>
          Type {word} to confirm
          <input
            value={phrase}
            onChange={e => setPhrase(e.target.value)}
            autoFocus
            aria-label={`Type ${word} to confirm`}
            autoComplete="off"
          />
        </label>
        {error && <p className={styles.error}>{error}</p>}
        {note && <p className={styles.note}>{note}</p>}
        <div className={styles.row}>
          <button type="button" onClick={onClose}>Close</button>
          <button type="button" className={styles.go} disabled={!ready || pending || !!note} onClick={submit}>
            {pending ? 'Recording…' : `Confirm ${word}`}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
