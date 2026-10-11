<script lang="ts">
  import {
    api,
    type Locale,
    type UsersApplyResult,
    type UsersView
  } from '../../lib/api'
  import { errorMessage, t } from '../../lib/i18n'
  import MutationResult from './MutationResult.svelte'

  let { locale, deviceId }: { locale: Locale; deviceId: string } = $props()

  const levels = ['Administrator', 'Operator', 'User']
  let view = $state<UsersView | undefined>()
  let failure = $state('')
  let busy = $state(false)
  let result = $state<UsersApplyResult | undefined>()
  let target = $state('')
  let targetLevel = $state('Administrator')
  let password = $state('')
  let confirmation = $state('')
  let newUsername = $state('')
  let newPassword = $state('')
  let newLevel = $state('User')
  let pendingDelete = $state('')

  $effect(() => {
    void load(deviceId)
  })

  async function load(id: string): Promise<void> {
    const response = await api.readUsers(id)
    view = response.data
    if (!response.data) {
      failure = response.error ? errorMessage(locale, response.error) : ''
      return
    }
    const users = response.data.users ?? []
    const current =
      users.find((user) => user.username === response.data?.current_username) ??
      users[0]
    if (current && !target) {
      target = current.username
      targetLevel = current.level
    }
  }

  function chooseTarget(username: string): void {
    target = username
    targetLevel =
      view?.users?.find((user) => user.username === username)?.level ??
      targetLevel
  }

  async function run(
    call: () => Promise<{
      data?: UsersApplyResult
      error?: { category: string; code: string; message: string }
    }>
  ): Promise<void> {
    busy = true
    failure = ''
    result = undefined
    const response = await call()
    busy = false
    if (response.error) failure = errorMessage(locale, response.error)
    if (response.data?.report) {
      result = response.data
      if (view && response.data.users)
        view = { ...view, users: response.data.users }
    }
  }

  async function changePassword(): Promise<void> {
    if (password !== confirmation) {
      failure = t(locale, 'users.mismatch')
      return
    }
    await run(() => api.setUser(deviceId, target, password, targetLevel))
    password = ''
    confirmation = ''
    await load(deviceId)
  }

  async function addUser(): Promise<void> {
    await run(() =>
      api.createUser(deviceId, newUsername.trim(), newPassword, newLevel)
    )
    newPassword = ''
  }

  async function deleteUser(username: string): Promise<void> {
    if (pendingDelete !== username) {
      pendingDelete = username
      return
    }
    pendingDelete = ''
    await run(() => api.deleteUser(deviceId, username))
  }
</script>

<section class="settings-section" aria-labelledby="users-heading">
  <h3 id="users-heading">{t(locale, 'users.title')}</h3>
  {#if view}
    {#if view.default_credential_warning}
      <p class="notice notice-warning" role="alert">
        {t(locale, 'users.defaultWarning')}
      </p>
    {/if}
    <ul class="user-list">
      {#each view.users ?? [] as user (user.username)}
        <li>
          <strong>{user.username}</strong>
          <span>{user.level}</span>
          {#if user.username === view.current_username}
            <span class="chip">{t(locale, 'users.signedIn')}</span>
          {:else}
            <button
              type="button"
              class="button button-secondary"
              disabled={busy}
              onclick={() => void deleteUser(user.username)}
              >{pendingDelete === user.username
                ? t(locale, 'users.confirmDelete')
                : t(locale, 'users.delete')}</button
            >
          {/if}
        </li>
      {/each}
    </ul>

    <h4>{t(locale, 'users.changeTitle')}</h4>
    <div class="settings-grid">
      <label class="field">
        <span>{t(locale, 'users.account')}</span>
        <select
          value={target}
          onchange={(event) =>
            chooseTarget((event.currentTarget as HTMLSelectElement).value)}
        >
          {#each view.users ?? [] as user (user.username)}
            <option value={user.username}>{user.username}</option>
          {/each}
        </select>
      </label>
      <label class="field">
        <span>{t(locale, 'users.newPassword')}</span>
        <input
          type="password"
          autocomplete="new-password"
          bind:value={password}
        />
      </label>
      <label class="field">
        <span>{t(locale, 'users.confirmPassword')}</span>
        <input
          type="password"
          autocomplete="new-password"
          bind:value={confirmation}
        />
      </label>
      <label class="field">
        <span>{t(locale, 'users.level')}</span>
        <select bind:value={targetLevel}>
          {#each levels as level (level)}
            <option value={level}>{level}</option>
          {/each}
        </select>
      </label>
    </div>
    <p class="field"><small>{t(locale, 'users.passwordRule')}</small></p>
    <div class="url-actions">
      <button
        type="button"
        class="button button-primary"
        disabled={busy || !password}
        onclick={() => void changePassword()}
        >{t(locale, 'users.change')}</button
      >
    </div>

    <h4>{t(locale, 'users.addTitle')}</h4>
    <div class="settings-grid">
      <label class="field">
        <span>{t(locale, 'users.username')}</span>
        <input type="text" autocomplete="off" bind:value={newUsername} />
      </label>
      <label class="field">
        <span>{t(locale, 'users.password')}</span>
        <input
          type="password"
          autocomplete="new-password"
          bind:value={newPassword}
        />
      </label>
      <label class="field">
        <span>{t(locale, 'users.level')}</span>
        <select bind:value={newLevel}>
          {#each levels as level (level)}
            <option value={level}>{level}</option>
          {/each}
        </select>
      </label>
    </div>
    <div class="url-actions">
      <button
        type="button"
        class="button button-secondary"
        disabled={busy || !newUsername || !newPassword}
        onclick={() => void addUser()}>{t(locale, 'users.add')}</button
      >
    </div>
  {/if}
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {/if}
  {#if result}
    {#if result.report.changes?.some( (change) => change.field.startsWith('password:') )}
      <p class="live-status" role="status">
        {result.sign_in_verified
          ? t(locale, 'users.signInVerified')
          : t(locale, 'users.signInFailed')}
      </p>
    {/if}
    <MutationResult {locale} report={result.report} />
  {/if}
</section>
