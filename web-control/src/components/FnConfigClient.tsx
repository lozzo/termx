"use client";

import { useState, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Loader2, Plus, Trash2, Pencil, Zap, RotateCcw, Save } from "lucide-react";
import Link from "next/link";

// ========== 类型定义（从 APK 端 fnPresets.ts 复制） ==========

interface FnItem {
  label: string;
  data: string;
  desc?: string;
}

interface FnGroup {
  name: string;
  items: FnItem[];
}

interface FnPreset {
  id: string;
  name: string;
  match: string[];
  groups: FnGroup[];
}

interface CustomProgram {
  id: string;
  name: string;
  match: string[];
  items: FnItem[];
}

interface FnConfig {
  customItems: FnItem[];
  programOverrides: {
    [presetId: string]: {
      hiddenLabels: string[];
      addedItems: FnItem[];
    };
  };
  customPrograms: CustomProgram[];
}

// ========== 内置预设（从 APK 端 fnPresets.ts 复制） ==========

const PROGRAM_PRESETS: FnPreset[] = [
  {
    id: "claude",
    name: "Claude Code",
    match: ["claude"],
    groups: [
      {
        name: "指令",
        items: [
          { label: "/clear", data: "/clear\n", desc: "清除上下文" },
          { label: "/compact", data: "/compact\n", desc: "压缩上下文" },
          { label: "/cost", data: "/cost\n", desc: "查看费用" },
          { label: "/help", data: "/help\n", desc: "帮助" },
          { label: "/review", data: "/review\n", desc: "代码审查" },
          { label: "/init", data: "/init\n", desc: "初始化" },
        ],
      },
      {
        name: "常用",
        items: [
          { label: "yes", data: "yes\n", desc: "确认" },
          { label: "no", data: "no\n", desc: "拒绝" },
          { label: "exit", data: "exit\n", desc: "退出" },
        ],
      },
      {
        name: "控制",
        items: [
          { label: "Ctrl+C", data: "\x03", desc: "中断" },
          { label: "Ctrl+D", data: "\x04", desc: "EOF" },
          { label: "Ctrl+L", data: "\x0c", desc: "清屏" },
        ],
      },
    ],
  },
  {
    id: "opencode",
    name: "OpenCode",
    match: ["opencode"],
    groups: [
      {
        name: "指令",
        items: [
          { label: "/clear", data: "/clear\n", desc: "清除上下文" },
          { label: "/compact", data: "/compact\n", desc: "压缩上下文" },
          { label: "/cost", data: "/cost\n", desc: "查看费用" },
          { label: "/help", data: "/help\n", desc: "帮助" },
        ],
      },
      {
        name: "常用",
        items: [
          { label: "yes", data: "yes\n", desc: "确认" },
          { label: "no", data: "no\n", desc: "拒绝" },
        ],
      },
      {
        name: "控制",
        items: [
          { label: "Ctrl+C", data: "\x03", desc: "中断" },
          { label: "Ctrl+D", data: "\x04", desc: "EOF" },
          { label: "Ctrl+L", data: "\x0c", desc: "清屏" },
        ],
      },
    ],
  },
];

// ========== 组件 ==========

function defaultConfig(): FnConfig {
  return { customItems: [], programOverrides: {}, customPrograms: [] };
}

interface FnConfigClientProps {
  initialConfig: FnConfig | null;
  hasSubscription: boolean;
}

type TabType = "custom" | "preset" | "programs";

export default function FnConfigClient({ initialConfig, hasSubscription }: FnConfigClientProps) {
  const [config, setConfig] = useState<FnConfig>(initialConfig || defaultConfig());
  const [activeTab, setActiveTab] = useState<TabType>("custom");
  const [activePresetId, setActivePresetId] = useState<string>(PROGRAM_PRESETS[0]?.id || "");
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [subscriptionDialogOpen, setSubscriptionDialogOpen] = useState(false);

  // Dialog state
  const [dialogOpen, setDialogOpen] = useState(false);
  const [dialogMode, setDialogMode] = useState<"add" | "edit">("add");
  const [dialogTarget, setDialogTarget] = useState<"custom" | "preset" | "program">("custom");
  const [dialogProgramId, setDialogProgramId] = useState<string>("");
  const [editIndex, setEditIndex] = useState(-1);
  const [formLabel, setFormLabel] = useState("");
  const [formData, setFormData] = useState("");
  const [formDesc, setFormDesc] = useState("");

  // Custom program dialog
  const [programDialogOpen, setProgramDialogOpen] = useState(false);
  const [programName, setProgramName] = useState("");
  const [programMatch, setProgramMatch] = useState("");
  const [editProgramId, setEditProgramId] = useState<string | null>(null);

  const updateConfig = useCallback((newConfig: FnConfig) => {
    setConfig(newConfig);
    setDirty(true);
  }, []);

  const handleSave = async () => {
    if (!hasSubscription) { setSubscriptionDialogOpen(true); return; }
    setSaving(true);
    try {
      const res = await fetch("/api/user/fn-config", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ config }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => null);
        if (data?.error === "subscription_required") {
          setSubscriptionDialogOpen(true);
          return;
        }
        throw new Error("保存失败");
      }
      setDirty(false);
    } catch (err) {
      console.error(err);
      alert("保存失败，请重试");
    } finally {
      setSaving(false);
    }
  };

  // ========== 全局自定义 ==========

  const openAddCustom = () => {
    setDialogMode("add");
    setDialogTarget("custom");
    setFormLabel("");
    setFormData("");
    setFormDesc("");
    setDialogOpen(true);
  };

  const openEditCustom = (idx: number) => {
    const item = config.customItems[idx];
    setDialogMode("edit");
    setDialogTarget("custom");
    setEditIndex(idx);
    setFormLabel(item.label);
    setFormData(item.data);
    setFormDesc(item.desc || "");
    setDialogOpen(true);
  };

  const deleteCustomItem = (idx: number) => {
    const items = [...config.customItems];
    items.splice(idx, 1);
    updateConfig({ ...config, customItems: items });
  };

  // ========== 内置预设 ==========

  const getPresetOverride = (presetId: string) => {
    return config.programOverrides[presetId] || { hiddenLabels: [], addedItems: [] };
  };

  const togglePresetItem = (presetId: string, label: string, visible: boolean) => {
    const override = { ...getPresetOverride(presetId) };
    if (visible) {
      override.hiddenLabels = override.hiddenLabels.filter((l) => l !== label);
    } else {
      override.hiddenLabels = [...override.hiddenLabels, label];
    }
    updateConfig({
      ...config,
      programOverrides: { ...config.programOverrides, [presetId]: override },
    });
  };

  const resetPreset = (presetId: string) => {
    const overrides = { ...config.programOverrides };
    delete overrides[presetId];
    updateConfig({ ...config, programOverrides: overrides });
  };

  const openAddPresetItem = (presetId: string) => {
    setDialogMode("add");
    setDialogTarget("preset");
    setDialogProgramId(presetId);
    setFormLabel("");
    setFormData("");
    setFormDesc("");
    setDialogOpen(true);
  };

  const deletePresetAddedItem = (presetId: string, idx: number) => {
    const override = { ...getPresetOverride(presetId) };
    const items = [...override.addedItems];
    items.splice(idx, 1);
    override.addedItems = items;
    updateConfig({
      ...config,
      programOverrides: { ...config.programOverrides, [presetId]: override },
    });
  };

  // ========== 自定义程序 ==========

  const openAddProgram = () => {
    setEditProgramId(null);
    setProgramName("");
    setProgramMatch("");
    setProgramDialogOpen(true);
  };

  const openEditProgram = (prog: CustomProgram) => {
    setEditProgramId(prog.id);
    setProgramName(prog.name);
    setProgramMatch(prog.match.join(", "));
    setProgramDialogOpen(true);
  };

  const saveProgram = () => {
    if (!programName.trim()) return;
    const matchArr = programMatch
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);

    if (editProgramId) {
      updateConfig({
        ...config,
        customPrograms: config.customPrograms.map((p) =>
          p.id === editProgramId ? { ...p, name: programName.trim(), match: matchArr } : p
        ),
      });
    } else {
      const newProg: CustomProgram = {
        id: Date.now().toString(36),
        name: programName.trim(),
        match: matchArr,
        items: [],
      };
      updateConfig({ ...config, customPrograms: [...config.customPrograms, newProg] });
    }
    setProgramDialogOpen(false);
  };

  const deleteProgram = (progId: string) => {
    if (!confirm("确定要删除此自定义程序？")) return;
    updateConfig({
      ...config,
      customPrograms: config.customPrograms.filter((p) => p.id !== progId),
    });
  };

  const openAddProgramItem = (progId: string) => {
    setDialogMode("add");
    setDialogTarget("program");
    setDialogProgramId(progId);
    setFormLabel("");
    setFormData("");
    setFormDesc("");
    setDialogOpen(true);
  };

  const openEditProgramItem = (progId: string, idx: number) => {
    const prog = config.customPrograms.find((p) => p.id === progId);
    if (!prog) return;
    const item = prog.items[idx];
    setDialogMode("edit");
    setDialogTarget("program");
    setDialogProgramId(progId);
    setEditIndex(idx);
    setFormLabel(item.label);
    setFormData(item.data);
    setFormDesc(item.desc || "");
    setDialogOpen(true);
  };

  const deleteProgramItem = (progId: string, idx: number) => {
    updateConfig({
      ...config,
      customPrograms: config.customPrograms.map((p) => {
        if (p.id !== progId) return p;
        const items = [...p.items];
        items.splice(idx, 1);
        return { ...p, items };
      }),
    });
  };

  // ========== Dialog 提交 ==========

  const handleDialogSave = () => {
    if (!formLabel.trim() || !formData) return;
    const item: FnItem = { label: formLabel.trim(), data: formData, desc: formDesc.trim() || undefined };

    if (dialogTarget === "custom") {
      const items = [...config.customItems];
      if (dialogMode === "edit") {
        items[editIndex] = item;
      } else {
        items.push(item);
      }
      updateConfig({ ...config, customItems: items });
    } else if (dialogTarget === "preset") {
      const override = { ...getPresetOverride(dialogProgramId) };
      override.addedItems = [...override.addedItems, item];
      updateConfig({
        ...config,
        programOverrides: { ...config.programOverrides, [dialogProgramId]: override },
      });
    } else if (dialogTarget === "program") {
      updateConfig({
        ...config,
        customPrograms: config.customPrograms.map((p) => {
          if (p.id !== dialogProgramId) return p;
          const items = [...p.items];
          if (dialogMode === "edit") {
            items[editIndex] = item;
          } else {
            items.push(item);
          }
          return { ...p, items };
        }),
      });
    }

    setDialogOpen(false);
  };

  // ========== 渲染辅助 ==========

  const displayData = (data: string) => {
    // 将控制字符转换为可读格式
    return data
      .replace(/\x03/g, "Ctrl+C")
      .replace(/\x04/g, "Ctrl+D")
      .replace(/\x0c/g, "Ctrl+L")
      .replace(/\x1a/g, "Ctrl+Z")
      .replace(/\x1c/g, "Ctrl+\\")
      .replace(/\x12/g, "Ctrl+R")
      .replace(/\x01/g, "Ctrl+A")
      .replace(/\x05/g, "Ctrl+E")
      .replace(/\x17/g, "Ctrl+W")
      .replace(/\x15/g, "Ctrl+U")
      .replace(/\x0b/g, "Ctrl+K")
      .replace(/\x1d/g, "Ctrl+]")
      .replace(/\n$/, "↵");
  };

  const renderItemRow = (
    item: FnItem,
    onEdit?: () => void,
    onDelete?: () => void
  ) => (
    <div className="flex items-center justify-between p-2.5 rounded-lg border border-zinc-800 bg-zinc-950/50">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-medium text-white text-sm">{item.label}</span>
          {item.desc && <span className="text-xs text-zinc-500">{item.desc}</span>}
        </div>
        <code className="text-xs text-zinc-400 mt-0.5 block">{displayData(item.data)}</code>
      </div>
      <div className="flex items-center gap-1 shrink-0">
        {onEdit && (
          <Button variant="ghost" size="icon" className="h-7 w-7 text-zinc-400 hover:text-white" onClick={onEdit}>
            <Pencil className="w-3.5 h-3.5" />
          </Button>
        )}
        {onDelete && (
          <Button variant="ghost" size="icon" className="h-7 w-7 text-zinc-400 hover:text-red-500" onClick={onDelete}>
            <Trash2 className="w-3.5 h-3.5" />
          </Button>
        )}
      </div>
    </div>
  );

  const tabs: { key: TabType; label: string }[] = [
    { key: "custom", label: "全局自定义" },
    { key: "preset", label: "内置预设" },
    { key: "programs", label: "自定义程序" },
  ];

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-3xl font-bold tracking-tight">快捷键配置</h2>
          <p className="text-zinc-400">管理终端快捷键和程序预设。</p>
        </div>
        <Button
          onClick={handleSave}
          disabled={saving || !dirty}
          className="bg-primary text-black hover:bg-primary/90"
        >
          {saving ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <Save className="w-4 h-4 mr-2" />}
          {dirty ? "保存更改" : "已保存"}
        </Button>
      </div>

      <div className="flex gap-6">
        {/* 左侧 Tab */}
        <div className="w-48 shrink-0 space-y-1">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className={`w-full text-left px-3 py-2 rounded-lg text-sm transition-colors ${
                activeTab === tab.key
                  ? "bg-zinc-800 text-white font-medium"
                  : "text-zinc-400 hover:text-white hover:bg-zinc-900"
              }`}
            >
              {tab.label}
            </button>
          ))}

          {activeTab === "preset" && (
            <div className="pt-2 pl-2 space-y-1">
              {PROGRAM_PRESETS.map((preset) => (
                <button
                  key={preset.id}
                  onClick={() => setActivePresetId(preset.id)}
                  className={`w-full text-left px-3 py-1.5 rounded text-xs transition-colors ${
                    activePresetId === preset.id
                      ? "bg-zinc-700 text-white"
                      : "text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800"
                  }`}
                >
                  {preset.name}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* 右侧内容 */}
        <div className="flex-1 min-w-0">
          {activeTab === "custom" && (
            <Card className="bg-zinc-900 border-zinc-800">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <CardTitle className="flex items-center gap-2">
                      <Zap className="w-5 h-5" />
                      全局自定义快捷键
                    </CardTitle>
                    <CardDescription>在所有终端会话中都可用的快捷键。</CardDescription>
                  </div>
                  <Button onClick={openAddCustom} className="bg-primary text-black hover:bg-primary/90" size="sm">
                    <Plus className="w-4 h-4 mr-1" />
                    添加
                  </Button>
                </div>
              </CardHeader>
              <CardContent>
                {config.customItems.length === 0 ? (
                  <div className="text-center py-8 text-zinc-500">
                    <Zap className="w-8 h-8 mx-auto mb-2 opacity-30" />
                    <p className="text-sm">暂无自定义快捷键</p>
                  </div>
                ) : (
                  <div className="space-y-2">
                    {config.customItems.map((item, idx) => (
                      <div key={idx}>
                        {renderItemRow(item, () => openEditCustom(idx), () => deleteCustomItem(idx))}
                      </div>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          )}

          {activeTab === "preset" && (() => {
            const preset = PROGRAM_PRESETS.find((p) => p.id === activePresetId);
            if (!preset) return null;
            const override = getPresetOverride(preset.id);
            const hiddenSet = new Set(override.hiddenLabels);

            return (
              <Card className="bg-zinc-900 border-zinc-800">
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <div>
                      <CardTitle>{preset.name}</CardTitle>
                      <CardDescription>
                        匹配规则: {preset.match.join(", ")}
                      </CardDescription>
                    </div>
                    <div className="flex items-center gap-2">
                      <Button onClick={() => openAddPresetItem(preset.id)} size="sm" variant="outline" className="border-zinc-700">
                        <Plus className="w-4 h-4 mr-1" />
                        新增
                      </Button>
                      <Button
                        onClick={() => resetPreset(preset.id)}
                        size="sm"
                        variant="ghost"
                        className="text-zinc-400 hover:text-white"
                      >
                        <RotateCcw className="w-4 h-4 mr-1" />
                        重置
                      </Button>
                    </div>
                  </div>
                </CardHeader>
                <CardContent className="space-y-4">
                  {preset.groups.map((group) => (
                    <div key={group.name}>
                      <h4 className="text-xs text-zinc-500 uppercase tracking-wider mb-2">{group.name}</h4>
                      <div className="space-y-1.5">
                        {group.items.map((item) => (
                          <div
                            key={item.label}
                            className="flex items-center justify-between p-2.5 rounded-lg border border-zinc-800 bg-zinc-950/50"
                          >
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className={`text-sm ${hiddenSet.has(item.label) ? "text-zinc-600 line-through" : "text-white font-medium"}`}>
                                  {item.label}
                                </span>
                                {item.desc && <span className="text-xs text-zinc-500">{item.desc}</span>}
                              </div>
                              <code className="text-xs text-zinc-400 mt-0.5 block">{displayData(item.data)}</code>
                            </div>
                            <Switch
                              checked={!hiddenSet.has(item.label)}
                              onCheckedChange={(checked) => togglePresetItem(preset.id, item.label, checked)}
                            />
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}

                  {override.addedItems.length > 0 && (
                    <div>
                      <h4 className="text-xs text-zinc-500 uppercase tracking-wider mb-2">自定义新增</h4>
                      <div className="space-y-1.5">
                        {override.addedItems.map((item, idx) => (
                          <div key={idx}>
                            {renderItemRow(item, undefined, () => deletePresetAddedItem(preset.id, idx))}
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </CardContent>
              </Card>
            );
          })()}

          {activeTab === "programs" && (
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-lg font-semibold">自定义程序</h3>
                  <p className="text-sm text-zinc-400">为特定程序定义专属快捷键，通过命令行匹配自动激活。</p>
                </div>
                <Button onClick={openAddProgram} className="bg-primary text-black hover:bg-primary/90" size="sm">
                  <Plus className="w-4 h-4 mr-1" />
                  新建程序
                </Button>
              </div>

              {config.customPrograms.length === 0 ? (
                <Card className="bg-zinc-900 border-zinc-800">
                  <CardContent className="py-8">
                    <div className="text-center text-zinc-500">
                      <Zap className="w-8 h-8 mx-auto mb-2 opacity-30" />
                      <p className="text-sm">暂无自定义程序</p>
                    </div>
                  </CardContent>
                </Card>
              ) : (
                config.customPrograms.map((prog) => (
                  <Card key={prog.id} className="bg-zinc-900 border-zinc-800">
                    <CardHeader>
                      <div className="flex items-center justify-between">
                        <div>
                          <CardTitle className="text-base">{prog.name}</CardTitle>
                          <CardDescription>
                            匹配规则: {prog.match.length > 0 ? prog.match.join(", ") : "未设置"}
                          </CardDescription>
                        </div>
                        <div className="flex items-center gap-2">
                          <Button onClick={() => openAddProgramItem(prog.id)} size="sm" variant="outline" className="border-zinc-700">
                            <Plus className="w-4 h-4 mr-1" />
                            添加
                          </Button>
                          <Button onClick={() => openEditProgram(prog)} size="sm" variant="ghost" className="text-zinc-400 hover:text-white">
                            <Pencil className="w-3.5 h-3.5" />
                          </Button>
                          <Button onClick={() => deleteProgram(prog.id)} size="sm" variant="ghost" className="text-zinc-400 hover:text-red-500">
                            <Trash2 className="w-3.5 h-3.5" />
                          </Button>
                        </div>
                      </div>
                    </CardHeader>
                    <CardContent>
                      {prog.items.length === 0 ? (
                        <p className="text-sm text-zinc-500 text-center py-4">暂无快捷键</p>
                      ) : (
                        <div className="space-y-1.5">
                          {prog.items.map((item, idx) => (
                            <div key={idx}>
                              {renderItemRow(
                                item,
                                () => openEditProgramItem(prog.id, idx),
                                () => deleteProgramItem(prog.id, idx)
                              )}
                            </div>
                          ))}
                        </div>
                      )}
                    </CardContent>
                  </Card>
                ))
              )}
            </div>
          )}
        </div>
      </div>

      {/* 添加/编辑快捷键 Dialog */}
      <Dialog open={dialogOpen} onOpenChange={(open) => { if (!open) setDialogOpen(false); }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{dialogMode === "add" ? "添加快捷键" : "编辑快捷键"}</DialogTitle>
            <DialogDescription>
              {dialogMode === "add" ? "添加一个新的快捷键项。" : "修改快捷键配置。"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="grid gap-2">
              <Label className="text-zinc-300">标签</Label>
              <Input
                placeholder="例如: npm run dev"
                value={formLabel}
                onChange={(e) => setFormLabel(e.target.value)}
                className="bg-black border-zinc-800 text-white"
              />
            </div>
            <div className="grid gap-2">
              <Label className="text-zinc-300">发送数据</Label>
              <Input
                placeholder='例如: npm run dev\n'
                value={formData}
                onChange={(e) => setFormData(e.target.value)}
                className="bg-black border-zinc-800 text-white font-mono"
              />
              <p className="text-xs text-zinc-500">使用 \n 表示回车，\x03 表示 Ctrl+C 等</p>
            </div>
            <div className="grid gap-2">
              <Label className="text-zinc-300">描述（可选）</Label>
              <Input
                placeholder="例如: 启动开发服务器"
                value={formDesc}
                onChange={(e) => setFormDesc(e.target.value)}
                className="bg-black border-zinc-800 text-white"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDialogOpen(false)} className="text-zinc-400">
              取消
            </Button>
            <Button
              onClick={handleDialogSave}
              className="bg-white text-black hover:bg-zinc-200"
              disabled={!formLabel.trim() || !formData}
            >
              {dialogMode === "add" ? "添加" : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 自定义程序 Dialog */}
      <Dialog open={programDialogOpen} onOpenChange={(open) => { if (!open) setProgramDialogOpen(false); }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{editProgramId ? "编辑程序" : "新建自定义程序"}</DialogTitle>
            <DialogDescription>设置程序名称和命令行匹配规则。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="grid gap-2">
              <Label className="text-zinc-300">程序名称</Label>
              <Input
                placeholder="例如: Vim"
                value={programName}
                onChange={(e) => setProgramName(e.target.value)}
                className="bg-black border-zinc-800 text-white"
              />
            </div>
            <div className="grid gap-2">
              <Label className="text-zinc-300">匹配规则</Label>
              <Input
                placeholder="例如: vim, nvim"
                value={programMatch}
                onChange={(e) => setProgramMatch(e.target.value)}
                className="bg-black border-zinc-800 text-white"
              />
              <p className="text-xs text-zinc-500">多个规则用逗号分隔，匹配终端中的程序名</p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setProgramDialogOpen(false)} className="text-zinc-400">
              取消
            </Button>
            <Button
              onClick={saveProgram}
              className="bg-white text-black hover:bg-zinc-200"
              disabled={!programName.trim()}
            >
              {editProgramId ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Subscription Required Dialog */}
      <Dialog open={subscriptionDialogOpen} onOpenChange={setSubscriptionDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>需要订阅</DialogTitle>
            <DialogDescription>
              保存快捷键配置需要有效订阅。升级后即可自定义快捷键。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setSubscriptionDialogOpen(false)} className="text-zinc-400">
              取消
            </Button>
            <Link href="/dashboard/billing">
              <Button className="bg-primary text-black">查看订阅计划</Button>
            </Link>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}