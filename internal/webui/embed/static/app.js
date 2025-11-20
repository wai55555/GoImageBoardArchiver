document.addEventListener('DOMContentLoaded', () => {
    // グローバルな状態管理
    let state = {
        config: null,
    };

    // DOM要素
    const globalSettingsContainer = document.getElementById('global-settings');
    const tasksContainer = document.getElementById('tasks-container');
    const addTaskBtn = document.getElementById('add-task-btn');
    const saveBtn = document.getElementById('save-btn');
    const statusMessage = document.getElementById('status-message');

    // 初期化関数
    async function initialize() {
        try {
            const response = await fetch('/api/config');
            if (!response.ok) {
                const errorData = await response.json();
                throw new Error(errorData.error || '設定の取得に失敗しました');
            }
            state.config = await response.json();
            renderForm();
        } catch (error) {
            showStatus(`初期設定の読み込み中にエラーが発生しました: ${error.message}`, 'error');
        }
    }

    // フォームを描画する関数
    function renderForm() {
        if (!state.config) return;

        // グローバル設定を描画
        renderGlobalSettings();

        // タスクを描画
        tasksContainer.innerHTML = ''; // コンテナをクリア
        state.config.tasks.forEach((task, index) => {
            const taskElement = createTaskElement(task, index);
            tasksContainer.appendChild(taskElement);
        });
    }
    
    function renderGlobalSettings() {
        globalSettingsContainer.innerHTML = '';
        // 簡単な例として一部のフィールドを描画
        globalSettingsContainer.appendChild(createFormGroup('global_max_concurrent_tasks', '最大並行タスク数', state.config.global_max_concurrent_tasks, 'number'));
        globalSettingsContainer.appendChild(createFormGroup('safety_stop_min_disk_gb', 'ディスク空き容量セーフティーストップ (GB)', state.config.safety_stop_min_disk_gb, 'number'));
    }

    // 個別のタスク要素を生成する関数
    function createTaskElement(task, index) {
        const div = document.createElement('div');
        div.className = 'task-box';
        div.dataset.index = index;
        
        const title = document.createElement('h3');
        title.textContent = `タスク: ${task.task_name || `新規タスク ${index + 1}`}`;
        div.appendChild(title);

        div.appendChild(createFormGroup(`task_name_${index}`, 'タスク名', task.task_name || '', 'text', true));
        div.appendChild(createFormGroup(`target_board_url_${index}`, '対象URL', task.target_board_url || '', 'url', true));
        div.appendChild(createFormGroup(`save_root_directory_${index}`, '保存先ルート', task.save_root_directory || '', 'text', true));
        
        const removeBtn = document.createElement('button');
        removeBtn.type = 'button';
        removeBtn.className = 'remove-task-btn';
        removeBtn.textContent = 'このタスクを削除';
        div.appendChild(removeBtn);

        return div;
    }

    function createFormGroup(id, label, value, type = 'text', required = false) {
        const group = document.createElement('div');
        group.className = 'form-group';
        
        const labelEl = document.createElement('label');
        labelEl.htmlFor = id;
        labelEl.textContent = label;
        
        const inputEl = document.createElement('input');
        inputEl.type = type;
        inputEl.id = id;
        inputEl.name = id;
        inputEl.value = value;
        if (required) {
            inputEl.required = true;
        }
        if (type === 'number') {
            inputEl.step = 'any';
        }

        group.appendChild(labelEl);
        group.appendChild(inputEl);
        return group;
    }

    // ステータスメッセージを表示するヘルパー関数
    function showStatus(message, type) {
        statusMessage.textContent = message;
        statusMessage.className = `status-message ${type}`;
        statusMessage.style.display = 'block';
    }

    // HTMLエスケープ関数 (XSS対策)
    function escapeHtml(text) {
        const div = document.createElement('div');
        div.appendChild(document.createTextNode(text || ''));
        return div.innerHTML;
    }

    // 保存処理
    async function handleSave() {
        showStatus('設定を保存中...', 'info');
        saveBtn.disabled = true;

        try {
            const newConfig = serializeFormToConfig();

            const response = await fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newConfig),
            });

            const result = await response.json();
            if (!response.ok) {
                throw new Error(result.error || '設定の保存に失敗しました');
            }

            showStatus(result.message, 'success');
            
            // サーバーにシャットダウンを通知
            setTimeout(() => {
                fetch('/api/shutdown', { method: 'POST' });
            }, 1000);

        } catch (error) {
            showStatus(`保存エラー: ${error.message}`, 'error');
            saveBtn.disabled = false; // エラー時は再試行可能にする
        }
    }

    // フォームの内容をJSONオブジェクトにシリアライズする
    function serializeFormToConfig() {
        const newConfig = { ...state.config };

        // グローバル設定の取得
        newConfig.global_max_concurrent_tasks = parseInt(document.getElementById('global_max_concurrent_tasks').value, 10);
        newConfig.safety_stop_min_disk_gb = parseFloat(document.getElementById('safety_stop_min_disk_gb').value);

        // タスク設定の取得
        newConfig.tasks = [];
        const taskBoxes = document.querySelectorAll('.task-box');
        taskBoxes.forEach((taskBox, index) => {
            // このタスクの元の設定を取得
            const originalTask = state.config.tasks[index] || {};
            const task = { ...originalTask }; // コピーを作成

            // フォームから値を読み取って上書き
            task.task_name = document.getElementById(`task_name_${index}`).value;
            task.target_board_url = document.getElementById(`target_board_url_${index}`).value;
            task.save_root_directory = document.getElementById(`save_root_directory_${index}`).value;
            
            // TODO: 他のフィールドも同様に取得
            
            newConfig.tasks.push(task);
        });

        return newConfig;
    }

    // 新しいタスクを追加
    function handleAddTask() {
        state.config.tasks.push({
            task_name: `新規タスク ${state.config.tasks.length + 1}`,
            site_adapter: "futaba", // デフォルト値
        });
        renderForm();
    }

    // タスクの削除
    function handleTaskAction(event) {
        if (event.target.classList.contains('remove-task-btn')) {
            const taskBox = event.target.closest('.task-box');
            const index = parseInt(taskBox.dataset.index, 10);
            if (!isNaN(index)) {
                state.config.tasks.splice(index, 1);
                renderForm();
            }
        }
    }

    // イベントリスナー
    saveBtn.addEventListener('click', handleSave);
    addTaskBtn.addEventListener('click', handleAddTask);
    tasksContainer.addEventListener('click', handleTaskAction);

    // 初期化実行
    initialize();
});