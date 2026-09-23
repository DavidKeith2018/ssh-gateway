import { ref } from 'vue'
export interface Decision {
  title: string
  message: string
  choices: string[]
  input?: string
}
export function useDecision() {
  const decision = ref<Decision | null>(null)
  let resolve: ((value: { choice: number; text: string }) => void) | undefined
  function finish(choice: number, text = '') {
    decision.value = null
    resolve?.({ choice, text })
    resolve = undefined
  }
  function ask(value: Decision) {
    if (resolve) finish(-1)
    decision.value = value
    return new Promise<{ choice: number; text: string }>((done) => {
      resolve = done
    })
  }
  return { decision, finish, ask }
}
