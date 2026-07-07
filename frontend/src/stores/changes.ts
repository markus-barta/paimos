import { defineStore } from "pinia";

export interface MutationChangeEvent {
  id: number;
  mutation_type: string;
  subject_type: string;
  subject_id: number;
  project_id: number | null;
  user_id: number | null;
  created_at: string;
}

type Predicate = (event: MutationChangeEvent) => boolean;
type Callback = (event: MutationChangeEvent) => void;

interface Subscription {
  predicate: Predicate;
  callback: Callback;
}

export const useChangesStore = defineStore("changes", () => {
  const subscriptions = new Map<number, Subscription>();
  let nextID = 1;

  function subscribe(predicate: Predicate, callback: Callback): () => void {
    const id = nextID++;
    subscriptions.set(id, { predicate, callback });
    return () => {
      subscriptions.delete(id);
    };
  }

  function publish(event: MutationChangeEvent) {
    for (const sub of subscriptions.values()) {
      if (sub.predicate(event)) sub.callback(event);
    }
  }

  return { subscribe, publish };
});
