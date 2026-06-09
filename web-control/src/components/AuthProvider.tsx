"use client";

import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  ReactNode,
} from "react";
import { useRouter } from "next/navigation";
import { getSafeClientRedirectPath } from "@/lib/url";

interface User {
  id: string;
  username: string;
  email: string;
  role: string;
}

interface AuthContextType {
  user: User | null;
  loading: boolean;
  login: (username: string, password: string, redirectTo?: string) => Promise<void>;
  register: (
    username: string,
    email: string,
    password: string,
    referralCode?: string,
    redirectTo?: string
  ) => Promise<void>;
  logout: () => Promise<void>;
  refreshUser: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const router = useRouter();

  const refreshUser = useCallback(async () => {
    try {
      // 服务端 getAuthFromRequest 会透明处理 JWT 过期：
      // JWT 有效 → 直接返回用户（不查库）
      // JWT 过期 → 用 refresh token cookie 自动签发新 JWT（查库）
      // 客户端不需要关心 token 刷新
      const res = await fetch("/api/auth/me", { cache: "no-store" });
      if (res.ok) {
        const data = await res.json();
        setUser(data.user);
      } else {
        setUser(null);
      }
    } catch {
      setUser(null);
    } finally {
      setLoading(false);
    }
  }, []);

  const finishAuth = useCallback(async (redirectTo?: string | null) => {
    const res = await fetch("/api/auth/me", { cache: "no-store" });
    if (!res.ok) {
      setUser(null);
      throw new Error("登录态写入失败，请刷新后重试");
    }

    const data = await res.json();
    setUser(data.user);
    router.replace(getSafeClientRedirectPath(redirectTo));
    router.refresh();
  }, [router]);

  useEffect(() => {
    refreshUser();
  }, [refreshUser]);

  const login = async (username: string, password: string, redirectTo?: string) => {
    const res = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });

    const data = await res.json();

    if (!res.ok) {
      throw new Error(data.error || "登录失败");
    }

    setUser(data.user);
    await finishAuth(redirectTo);
  };

  const register = async (
    username: string,
    email: string,
    password: string,
    referralCode?: string,
    redirectTo?: string
  ) => {
    const res = await fetch("/api/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, email, password, referralCode }),
    });

    const data = await res.json();

    if (!res.ok) {
      throw new Error(data.error || "注册失败");
    }

    setUser(data.user);
    await finishAuth(redirectTo || "/dashboard");
  };

  const logout = async () => {
    await fetch("/api/auth/logout", { method: "POST" });
    setUser(null);
    router.push("/login");
  };

  return (
    <AuthContext.Provider
      value={{ user, loading, login, register, logout, refreshUser }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
