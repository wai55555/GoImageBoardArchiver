document.addEventListener('DOMContentLoaded', () => {
    // グローバルな状態管理
    let state = {
        config: null,
    };

    // DOM要素のキャッシュ
    const dom = {
        globalSettingsContainer: document.getElementById('global-settings'),
        tasksContainer: document.getElementById('tasks-container'),
        addTaskBtn: document.getElementById('add-task-btn'),
        saveBtn: document.getElementById('save-btn'),
        statusMessage: document.getElementById('status-message'),
    };

    // =================================================================
    // 初期化
    // =================================================================
    async function initialize() {
        try {
            const response = await fetch('/api/config');
            if (!response.ok) {
                const errorData = await response.json();
                throw new Error(errorData.error || '設定の取得に失敗しました');
            }
            state.config = await response.json();
            // デフォルト値のフォールバック
            if (!state.config.tasks) state.config.tasks = [];
            if (!state.config.network) state.config.network = {};

            renderForm();
            attachEventListeners();
        } catch (error) {
            showStatus(`初期設定の読み込み中にエラーが発生しました: ${error.message}`, 'error');
        }
    }

    // =================================================================
    // イベントリスナーの設定
    // =================================================================
    function attachEventListeners() {
        dom.saveBtn.addEventListener('click', handleSave);
        dom.addTaskBtn.addEventListener('click', handleAddTask);
        
        // イベント委譲を使用して動的に生成される要素のイベントを処理
        document.body.addEventListener('click', (e) => {
            if (e.target.classList.contains('accordion-header')) {
                e.target.parentElement.classList.toggle('open');
            }
            if (e.target.classList.contains('remove-task-btn')) {
                handleRemoveTask(e);
            }
            if (e.target.classList.contains('clone-task-btn')) {
                handleCloneTask(e);
            }
        });
    }

    // =================================================================
    // レンダリング関連
    // =================================================================
    function renderForm() {
        if (!state.config) return;
        renderGlobalSettings();
        renderTasks();
    }

    function renderGlobalSettings() {
        const { config } = state;
        const container = dom.globalSettingsContainer;
        container.innerHTML = ''; // クリア

        const general = createAccordion('global-general', '一般設定', true);
        general.appendChild(createFormGroup('global_save_root_directory', 'デフォルト保存先ルート', config.global_save_root_directory, 'text', '全タスクの保存先ルートディレクトリのデフォルト値。'));
        general.appendChild(createFormGroup('global_max_concurrent_tasks', '最大並行タスク数', config.global_max_concurrent_tasks, 'number', '同時に実行する最大タスク数。'));
        general.appendChild(createFormGroup('safety_stop_min_disk_gb', 'ディスク空き容量セーフティーストップ (GB)', config.safety_stop_min_disk_gb, 'number', 'ディスクの空き容量がこの値を下回ると、新規のダウンロードを停止します。'));
        general.appendChild(createFormGroup('web_ui_theme', 'Web UIテーマ', config.web_ui_theme, 'select', 'UIのテーマを選択します。', ['auto', 'light', 'dark']));
        container.appendChild(general);

        const network = createAccordion('global-network', 'ネットワーク設定');
        network.appendChild(createFormGroup('network_user_agent', 'User-Agent', config.network.user_agent, 'text', 'HTTPリクエスト時に使用するUser-Agent文字列。'));
        // TODO: default_headers, per_domain_interval_ms のUIを追加
        container.appendChild(network);

        const notification = createAccordion('global-notification', 'ログと通知');
        notification.appendChild(createFormGroup('enable_log_file', 'ログファイルを有効にする', config.enable_log_file, 'checkbox', 'ログをファイルに書き出します。'));
        notification.appendChild(createFormGroup('log_file_path', 'ログファイルパス', config.log_file_path, 'text', 'ログファイルのパス。空の場合は `giba_[日付].log` が使用されます。'));
        notification.appendChild(createFormGroup('notification_webhook_url', '通知用Webhook URL', config.notification_webhook_url, 'url', 'タスク完了・エラー時に通知を送るWebhook URL。'));
        container.appendChild(notification);
    }

    function renderTasks() {
        const container = dom.tasksContainer;
        container.innerHTML = '';
        state.config.tasks.forEach((task, index) => {
            container.appendChild(createTaskElement(task, index));
        });
    }

    function createTaskElement(task, index) {
        const accordion = createAccordion(`task-${index}`, `タスク: ${task.task_name || `新規タスク ${index + 1}`}`, false, true);
        accordion.dataset.index = index;

        // ヘッダーにボタンを追加
        const header = accordion.querySelector('.accordion-header');
        header.innerHTML = `
            <span class="drag-handle">☰</span>
            <label class="switch">
                <input type="checkbox" class="task-enabled-switch" ${task.enabled ? 'checked' : ''}>
                <span class="slider round"></span>
            </label>
            <span class="accordion-title">${escapeHtml(task.task_name || `新規タスク ${index + 1}`)}</span>
            <div class="header-buttons">
                <button type="button" class="clone-task-btn" title="複製">❐</button>
                <button type="button" class="remove-task-btn" title="削除">×</button>
            </div>
        `;

        const basic = createAccordion('task-basic', '基本設定', true);
        basic.appendChild(createFormGroup(`task_name_${index}`, 'タスク名', task.task_name, 'text', 'タスクを識別するための名前。', null, true));
        basic.appendChild(createFormGroup(`site_adapter_${index}`, 'サイトアダプター', task.site_adapter, 'select', '対象サイトの種類を選択します。', ['futaba'], true));
        basic.appendChild(createFormGroup(`target_board_url_${index}`, '対象URL', task.target_board_url, 'url', 'アーカイブ対象の掲示板URL。', null, true));
        basic.appendChild(createFormGroup(`save_root_directory_${index}`, '保存先ルート', task.save_root_directory, 'text', 'このタスクのファイルを保存する場所。空の場合はグローバル設定が使用されます。'));
        basic.appendChild(createFormGroup(`watch_interval_ms_${index}`, '監視間隔 (分)', task.watch_interval_ms / 60000, 'number', '監視モード時に次のチェックを行うまでの時間（分）。'));
        accordion.appendChild(basic);

        const filter = createAccordion('task-filter', 'フィルタリング');
        // search_keywords（複数、OR検索）がある場合はそちらを表示に使い、
        // 無ければ旧来のsearch_keyword（単一）にフォールバックする。
        // 画面上はカンマ区切りの1つの入力欄として扱う。
        const searchKeywordDisplay = (task.search_keywords && task.search_keywords.length > 0)
            ? task.search_keywords.join(', ')
            : (task.search_keyword || '');
        filter.appendChild(createFormGroup(`search_keyword_${index}`, '検索キーワード', searchKeywordDisplay, 'text', 'スレッドタイトルにこれらのキーワードのいずれかが含まれるものを対象とします（カンマ区切りでOR検索。例: AI, 猫, 犬）。空欄なら全スレッドが対象です。'));
        // TODO: exclude_keywords, minimum_media_count, PostContentFilters のUIを追加
        accordion.appendChild(filter);
        
        const advanced = createAccordion('task-advanced', '高度な設定');
        advanced.appendChild(createFormGroup(`directory_format_${index}`, 'ディレクトリ形式', task.directory_format, 'text', '保存ディレクトリ名のフォーマット。使用可能な変数: {thread_id}, {thread_title_safe}, {year}, {month}, {day}'));
        // TODO: 他の高度な設定項目を追加
        accordion.appendChild(advanced);

        return accordion;
    }

    // =================================================================
    // UI要素生成ヘルパー
    // =================================================================
    function createAccordion(id, title, isOpen = false, isTask = false) {
        const div = document.createElement('div');
        div.className = `accordion ${isOpen ? 'open' : ''} ${isTask ? 'task-box' : ''}`;
        div.id = id;
        div.innerHTML = `
            <div class="accordion-header">
                <span class="accordion-title">${title}</span>
            </div>
            <div class="accordion-content"></div>
        `;
        return div;
    }

    function createFormGroup(id, label, value, type = 'text', helpText = '', options = null, required = false) {
        const group = document.createElement('div');
        group.className = 'form-group';
        
        const labelEl = document.createElement('label');
        labelEl.htmlFor = id;
        labelEl.textContent = label;
        
        if (helpText) {
            const helpIcon = document.createElement('span');
            helpIcon.className = 'help-icon';
            helpIcon.textContent = '(?)';
            helpIcon.title = helpText;
            labelEl.appendChild(helpIcon);
        }
        
        let inputEl;
        if (type === 'select') {
            inputEl = document.createElement('select');
            options.forEach(opt => {
                const optionEl = document.createElement('option');
                optionEl.value = opt;
                optionEl.textContent = opt;
                if (opt === value) optionEl.selected = true;
                inputEl.appendChild(optionEl);
            });
        } else if (type === 'checkbox') {
            inputEl = document.createElement('input');
            inputEl.type = 'checkbox';
            inputEl.checked = value;
        } else {
            inputEl = document.createElement('input');
            inputEl.type = type;
            // config側でomitemptyによりキーが省略されていると value は undefined になる。
            // そのまま代入すると input.value に文字列 "undefined" が入り、保存時にconfig.jsonへ
            // そのまま書き込まれてしまうため、未設定は空文字として扱う。
            inputEl.value = (value === undefined || value === null) ? '' : value;
            if (type === 'number') inputEl.step = 'any';
        }
        
        inputEl.id = id;
        inputEl.name = id;
        if (required) inputEl.required = true;

        group.appendChild(labelEl);
        group.appendChild(inputEl);
        return group;
    }

    // =================================================================
    // イベントハンドラ
    // =================================================================
    async function handleSave() {
        showStatus('設定を保存中...', 'info');
        dom.saveBtn.disabled = true;

        try {
            const newConfig = serializeFormToConfig();
            const response = await fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newConfig),
            });
            const result = await response.json();
            if (!response.ok) throw new Error(result.error || '設定の保存に失敗しました');
            
            showStatus(result.message, 'success');
            setTimeout(() => fetch('/api/shutdown', { method: 'POST' }), 1000);
        } catch (error) {
            showStatus(`保存エラー: ${error.message}`, 'error');
            dom.saveBtn.disabled = false;
        }
    }

    function handleAddTask() {
        // task_templates.futaba_default を新規タスクのひな形として使う。
        // directory_format等、まだこのフォームに項目がない設定値も、
        // ここでuse_templateを付けておけばGo側(config.LoadAndResolve)が
        // 次回読み込み時にテンプレートから補完してくれる。
        // テンプレートが存在しない/削除されている場合に備え、存在チェックしてから使う
        // （「設定に無いものは無制限」ではなく、無ければ使わない）。
        const templateName = 'futaba_default';
        const template = state.config.task_templates && state.config.task_templates[templateName];

        // 深いコピーにしないと、複数の新規タスクがfutaba_catalog_settings等の
        // ネストしたオブジェクトを共有してしまい、片方の編集がもう片方に影響する
        // (handleCloneTaskと同じ手法)。
        const newTask = template ? JSON.parse(JSON.stringify(template)) : {};

        newTask.enabled = true;
        newTask.task_name = `新規タスク ${state.config.tasks.length + 1}`;
        newTask.site_adapter = newTask.site_adapter || 'futaba';
        newTask.watch_interval_ms = 900000; // 15分
        if (template) {
            newTask.use_template = templateName;
        }
        // 対象URL・保存先ルートは、このプロジェクトで実際によく使われる値を
        // 初期値として入れておく（ユーザーが必要に応じて書き換える前提）。
        // 検索条件はスレッドごとに毎回変わるため、流用できる妥当なデフォルトが
        // 存在せず空のままにする。
        newTask.target_board_url = 'https://may.2chan.net/b/';
        newTask.save_root_directory = './downloads/';
        newTask.search_keyword = '';
        newTask.search_keywords = [];

        state.config.tasks.push(newTask);
        renderTasks();
    }

    function handleRemoveTask(e) {
        const taskBox = e.target.closest('.task-box');
        const index = parseInt(taskBox.dataset.index, 10);
        if (!isNaN(index) && confirm(`タスク「${state.config.tasks[index].task_name}」を削除しますか？`)) {
            state.config.tasks.splice(index, 1);
            renderTasks();
        }
    }

    function handleCloneTask(e) {
        const taskBox = e.target.closest('.task-box');
        const index = parseInt(taskBox.dataset.index, 10);
        if (!isNaN(index)) {
            const originalTask = state.config.tasks[index];
            const clonedTask = JSON.parse(JSON.stringify(originalTask)); // Deep copy
            clonedTask.task_name = `${originalTask.task_name} (コピー)`;
            state.config.tasks.splice(index + 1, 0, clonedTask);
            renderTasks();
        }
    }

    // =================================================================
    // シリアライズ / ユーティリティ
    // =================================================================
    function serializeFormToConfig() {
        const newConfig = { ...state.config };

        // グローバル設定
        newConfig.global_save_root_directory = document.getElementById('global_save_root_directory').value;
        newConfig.global_max_concurrent_tasks = parseInt(document.getElementById('global_max_concurrent_tasks').value, 10);
        newConfig.safety_stop_min_disk_gb = parseFloat(document.getElementById('safety_stop_min_disk_gb').value);
        newConfig.web_ui_theme = document.getElementById('web_ui_theme').value;
        newConfig.enable_log_file = document.getElementById('enable_log_file').checked;
        newConfig.log_file_path = document.getElementById('log_file_path').value;
        newConfig.notification_webhook_url = document.getElementById('notification_webhook_url').value;
        
        // タスク設定
        newConfig.tasks = [];
        const taskBoxes = document.querySelectorAll('.task-box');
        taskBoxes.forEach((taskBox, i) => {
            const originalTask = state.config.tasks[i] || {};
            const task = { ...originalTask };

            task.enabled = taskBox.querySelector('.task-enabled-switch').checked;
            task.task_name = document.getElementById(`task_name_${i}`).value;
            task.site_adapter = document.getElementById(`site_adapter_${i}`).value;
            task.target_board_url = document.getElementById(`target_board_url_${i}`).value;
            task.save_root_directory = document.getElementById(`save_root_directory_${i}`).value;
            task.watch_interval_ms = parseFloat(document.getElementById(`watch_interval_ms_${i}`).value) * 60000;
            // カンマ区切りの入力を、OR検索用のsearch_keywords配列にパースする
            // （半角・全角カンマの両方に対応。前後の空白と空要素は除去）。
            const searchKeywordInput = document.getElementById(`search_keyword_${i}`).value;
            task.search_keywords = searchKeywordInput
                .split(/[,、]/)
                .map(s => s.trim())
                .filter(s => s.length > 0);
            // 表示用に組み立てた旧フィールドはこの画面ではもう使わないため空にする
            // （search_keywordsが空の場合、Go側はsearch_keywordにフォールバックする設計のため、
            //   古い値が残っていると意図せず復活してしまう）。
            task.search_keyword = '';
            task.directory_format = document.getElementById(`directory_format_${i}`).value;
            
            newConfig.tasks.push(task);
        });

        return newConfig;
    }

    function showStatus(message, type) {
        dom.statusMessage.textContent = message;
        dom.statusMessage.className = `status-message ${type}`;
        dom.statusMessage.style.display = 'block';
        if (type !== 'info') {
            setTimeout(() => { dom.statusMessage.style.display = 'none'; }, 5000);
        }
    }

    function escapeHtml(text) {
        const div = document.createElement('div');
        div.appendChild(document.createTextNode(text || ''));
        return div.innerHTML;
    }

    // =================================================================
    // 初期化実行
    // =================================================================
    initialize();
});
