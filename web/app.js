    const loginCard = document.querySelector('#login-card');
    const configCard = document.querySelector('#config-card');
    const mountList = document.querySelector('#mount-list');
    const configStatus = document.querySelector('#config-status');
    const maxMounts = 20;

    async function request(path, options = {}) {
      const response = await fetch(path, {
        credentials: 'same-origin',
        cache: 'no-store',
        ...options,
        headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
      });
      if (!response.ok) {
        const message = (await response.text()).trim();
        if (response.status === 401) showLogin();
        throw new Error(message || `请求失败 (${response.status})`);
      }
      if (response.status === 204) return {};
      return response.json();
    }

    function setStatus(element, message, kind = '') {
      element.textContent = message;
      element.className = `status${kind ? ` ${kind}` : ''}`;
    }

    function showLogin() {
      configCard.classList.add('hidden');
      loginCard.classList.remove('hidden');
      document.querySelector('#login-password').focus();
    }

    function showConfig() {
      loginCard.classList.add('hidden');
      configCard.classList.remove('hidden');
    }

    function addMount(mount = {}) {
      if (mountList.children.length >= maxMounts) {
        setStatus(configStatus, `最多支持 ${maxMounts} 个挂载项`, 'error');
        return;
      }
      const card = document.createElement('article');
      card.className = 'mount-card';
      card.innerHTML = `
        <div class="mount-head"><div class="mount-title"><span class="mount-number"></span><span>目录映射</span></div><button type="button" class="remove-button">移除</button></div>
        <div class="grid">
          <div class="field full"><label>源目录</label><input data-field="source" type="text" required autocomplete="off" spellcheck="false" placeholder="/vol02/1000-0-1fd91f9b/Picture"></div>
          <div class="field full"><label>本地目标目录</label><input data-field="target" type="text" required autocomplete="off" spellcheck="false" placeholder="/vol1/1000/Photos"></div>
          <div class="field"><label>检查文件名</label><input data-field="checkFile" type="text" required autocomplete="off" spellcheck="false" value="remote" pattern="[A-Za-z0-9._-]+"><span class="help">在源目录根部检查此文件，默认 remote</span></div>
        </div>`;
      card.querySelector('[data-field="source"]').value = mount.source || '';
      card.querySelector('[data-field="target"]').value = mount.target || '';
      card.querySelector('[data-field="checkFile"]').value = mount.checkFile || 'remote';
      card.querySelector('.remove-button').addEventListener('click', () => {
        if (mountList.children.length <= 1) {
          setStatus(configStatus, '至少保留一个挂载项', 'error');
          return;
        }
        card.remove();
        renumberMounts();
      });
      mountList.append(card);
      renumberMounts();
    }

    function renumberMounts() {
      [...mountList.children].forEach((card, index) => {
        card.querySelector('.mount-number').textContent = String(index + 1).padStart(2, '0');
      });
      document.querySelector('#add-mount').disabled = mountList.children.length >= maxMounts;
    }

    async function loadConfig() {
      const config = await request('/api/config');
      mountList.replaceChildren();
      (config.mounts || []).forEach(addMount);
      if (mountList.children.length === 0) addMount();
      document.querySelector('#interval').value = config.intervalSeconds || 30;
      showConfig();
      if (config.warning) setStatus(configStatus, config.warning, 'error');
      else setStatus(configStatus, '配置已加载');
    }

    document.querySelector('#login-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const button = event.currentTarget.querySelector('button[type="submit"]');
      const status = document.querySelector('#login-status');
      button.disabled = true;
      setStatus(status, '正在登录…');
      try {
        await request('/api/login', { method: 'POST', body: JSON.stringify({ password: document.querySelector('#login-password').value }) });
        document.querySelector('#login-password').value = '';
        await loadConfig();
      } catch (error) {
        setStatus(status, error.message, 'error');
      } finally {
        button.disabled = false;
      }
    });

    document.querySelector('#add-mount').addEventListener('click', () => addMount());

    document.querySelector('#config-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const button = document.querySelector('#save-config');
      if (!event.currentTarget.reportValidity()) return;
      const mounts = [...mountList.children].map((card) => ({
        source: card.querySelector('[data-field="source"]').value.trim(),
        target: card.querySelector('[data-field="target"]').value.trim(),
        checkFile: card.querySelector('[data-field="checkFile"]').value.trim(),
      }));
      button.disabled = true;
      setStatus(configStatus, '正在保存…');
      try {
        const result = await request('/api/config', {
          method: 'POST',
          body: JSON.stringify({ mounts, intervalSeconds: Number(document.querySelector('#interval').value) }),
        });
        setStatus(configStatus, result.message || '配置已保存', 'success');
      } catch (error) {
        setStatus(configStatus, error.message, 'error');
      } finally {
        button.disabled = false;
      }
    });

    document.querySelector('#logout').addEventListener('click', async () => {
      try { await request('/api/logout', { method: 'POST', body: '{}' }); } catch (_) { /* The session may already have expired. */ }
      showLogin();
      setStatus(document.querySelector('#login-status'), '已退出登录');
    });

    document.querySelector('#password-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const status = document.querySelector('#password-status');
      const button = event.currentTarget.querySelector('button[type="submit"]');
      const newPassword = document.querySelector('#new-password').value;
      if (newPassword.length < 16 || newPassword.length > 128) {
        setStatus(status, '密码长度必须为 16 到 128 个字符', 'error');
        return;
      }
      button.disabled = true;
      setStatus(status, '正在更新…');
      try {
        const result = await request('/api/password', { method: 'POST', body: JSON.stringify({ newPassword }) });
        document.querySelector('#new-password').value = '';
        showLogin();
        setStatus(document.querySelector('#login-status'), result.message || '请使用新密码登录', 'success');
      } catch (error) {
        setStatus(status, error.message, 'error');
      } finally {
        button.disabled = false;
      }
    });

    loadConfig().catch(() => showLogin());
