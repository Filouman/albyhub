import React, { useState } from "react";
import {
  RequestMethodType,
  budgetOptions,
  expiryOptions,
  nip47MethodDescriptions,
  iconMap,
  BudgetRenewalType,
  validBudgetRenewals,
} from "src/types";

interface PermissionsProps {
  initialRequestMethods: Set<RequestMethodType>;
  initialMaxAmount: number;
  initialBudgetRenewal: BudgetRenewalType;
  initialExpiresAt?: Date;
  onRequestMethodChange: (methods: Set<RequestMethodType>) => void;
  onMaxAmountChange: (amount: number) => void;
  onBudgetRenewalChange: (renewal: BudgetRenewalType) => void;
  onExpiresAtChange: (date?: Date) => void;
  budgetUsage?: number;
  isEditing: boolean;
  isNew?: boolean;
}

const Permissions: React.FC<PermissionsProps> = ({
  initialRequestMethods,
  initialMaxAmount,
  initialBudgetRenewal,
  initialExpiresAt,
  onRequestMethodChange,
  onMaxAmountChange,
  onBudgetRenewalChange,
  onExpiresAtChange,
  isEditing,
  isNew,
  budgetUsage,
}) => {
  const [requestMethods, setRequestMethods] = useState(initialRequestMethods);
  const [maxAmount, setMaxAmount] = useState(initialMaxAmount);
  const [budgetRenewal, setBudgetRenewal] = useState<
    BudgetRenewalType | "never"
  >(initialBudgetRenewal || "never");
  const [expiresAt, setExpiresAt] = useState(initialExpiresAt);
  const [days, setDays] = useState(isNew ? 0 : -1);
  const [expireOptions, setExpireOptions] = useState(!isNew);

  const handleRequestMethodChange = (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    const requestMethod = event.target.value as RequestMethodType;
    const newRequestMethods = new Set(requestMethods);
    if (newRequestMethods.has(requestMethod)) {
      newRequestMethods.delete(requestMethod);
    } else {
      newRequestMethods.add(requestMethod);
    }
    setRequestMethods(newRequestMethods);
    onRequestMethodChange(newRequestMethods);
  };

  const handleMaxAmountChange = (amount: number) => {
    setMaxAmount(amount);
    onMaxAmountChange(amount);
  };

  const handleBudgetRenewalChange = (
    event: React.ChangeEvent<HTMLSelectElement>
  ) => {
    const renewal = event.target.value as BudgetRenewalType;
    setBudgetRenewal(renewal);
    onBudgetRenewalChange(renewal);
  };

  const handleDaysChange = (days: number) => {
    setDays(days);
    if (!days) {
      setExpiresAt(undefined);
      onExpiresAtChange(undefined);
      return;
    }
    const currentDate = new Date();
    const expiryDate = new Date(
      Date.UTC(
        currentDate.getUTCFullYear(),
        currentDate.getUTCMonth(),
        currentDate.getUTCDate() + days,
        23,
        59,
        59,
        0
      )
    );
    setExpiresAt(expiryDate);
    onExpiresAtChange(expiryDate);
  };

  return (
    <div>
      <div className="mb-6">
        <ul className="flex flex-col w-full">
          {(Object.keys(nip47MethodDescriptions) as RequestMethodType[]).map(
            (rm, index) => {
              const RequestMethodIcon = iconMap[rm];
              return (
                <li
                  key={index}
                  className={`w-full ${
                    rm == "pay_invoice" ? "order-last" : ""
                  } ${!isEditing && !requestMethods.has(rm) ? "hidden" : ""}`}
                >
                  <div className="flex items-center mb-2">
                    {RequestMethodIcon && (
                      <RequestMethodIcon
                        className={`text-gray-800 dark:text-gray-300 w-4 mr-3 ${
                          isEditing ? "hidden" : ""
                        }`}
                      />
                    )}
                    <input
                      type="checkbox"
                      id={rm}
                      value={rm}
                      checked={requestMethods.has(rm)}
                      onChange={handleRequestMethodChange}
                      className={`${
                        !isEditing ? "hidden" : ""
                      } w-4 h-4 mr-4 text-indigo-500 bg-gray-50 border border-gray-300 rounded focus:ring-indigo-500 dark:focus:ring-indigo-400 dark:ring-offset-gray-800 focus:ring-2 dark:bg-surface-00dp dark:border-gray-700 cursor-pointer`}
                    />
                    <label
                      htmlFor={rm}
                      className="text-gray-800 dark:text-gray-300 cursor-pointer"
                    >
                      {nip47MethodDescriptions[rm]}
                    </label>
                  </div>
                  {rm == "pay_invoice" && (
                    <div
                      className={`pt-2 pb-2 pl-5 ml-2.5 border-l-2 border-l-gray-200 dark:border-l-gray-400 ${
                        !requestMethods.has(rm)
                          ? isEditing
                            ? "pointer-events-none opacity-30"
                            : "hidden"
                          : ""
                      }`}
                    >
                      {isEditing ? (
                        <>
                          <p className="text-gray-600 dark:text-gray-300 mb-3 text-sm capitalize">
                            Budget Renewal:
                            {!isEditing ? (
                              budgetRenewal
                            ) : (
                              <select
                                id="budgetRenewal"
                                value={budgetRenewal}
                                onChange={handleBudgetRenewalChange}
                                className="bg-gray-50 border border-gray-300 text-gray-900 text-sm rounded-lg focus:ring-indigo-500 focus:border-indigo-500 ml-2 p-2.5 pr-10 dark:bg-gray-700 dark:border-gray-600 dark:placeholder-gray-400 dark:text-white dark:focus:ring-indigo-400 dark:focus:border-indigo-400"
                                disabled={!isEditing}
                              >
                                {validBudgetRenewals.map((renewalOption) => (
                                  <option
                                    key={renewalOption || "never"}
                                    value={renewalOption || "never"}
                                  >
                                    {renewalOption
                                      ? renewalOption.charAt(0).toUpperCase() +
                                        renewalOption.slice(1)
                                      : "Never"}
                                  </option>
                                ))}
                              </select>
                            )}
                          </p>
                          <div
                            id="budget-allowance-limits"
                            className="grid grid-cols-6 grid-rows-2 md:grid-rows-1 md:grid-cols-6 gap-2 text-xs text-gray-800 dark:text-neutral-200"
                          >
                            {Object.keys(budgetOptions).map((budget) => {
                              return (
                                <div
                                  key={budget}
                                  onClick={() =>
                                    handleMaxAmountChange(budgetOptions[budget])
                                  }
                                  className={`col-span-2 md:col-span-1 cursor-pointer rounded border-2 ${
                                    maxAmount == budgetOptions[budget]
                                      ? "border-indigo-500 dark:border-indigo-400 text-indigo-500 bg-indigo-100 dark:bg-indigo-900"
                                      : "border-gray-200 dark:border-gray-400"
                                  } text-center py-4 dark:text-white`}
                                >
                                  {budget}
                                  <br />
                                  {budgetOptions[budget] ? "sats" : "#reckless"}
                                </div>
                              );
                            })}
                          </div>
                        </>
                      ) : isNew ? (
                        <>
                          <p className="text-gray-600 dark:text-gray-300 text-sm">
                            <span className="capitalize">{budgetRenewal}</span>{" "}
                            budget: {maxAmount} sats
                          </p>
                        </>
                      ) : (
                        <table className="text-gray-600 dark:text-neutral-400">
                          <tbody>
                            <tr className="text-sm">
                              <td className="pr-2">Budget Allowance:</td>
                              <td>
                                {maxAmount || "∞"} sats ({budgetUsage} sats
                                used)
                              </td>
                            </tr>
                            <tr className="text-sm">
                              <td className="pr-2">Renews:</td>
                              <td className="capitalize">
                                {budgetRenewal || "Never"}
                              </td>
                            </tr>
                          </tbody>
                        </table>
                      )}
                    </div>
                  )}
                </li>
              );
            }
          )}
        </ul>
      </div>

      {(isNew ? !expiresAt || days : isEditing) ? (
        <>
          <div
            onClick={() => setExpireOptions(true)}
            className={`${
              expireOptions ? "hidden" : ""
            } cursor-pointer text-sm font-medium text-indigo-500  dark:text-indigo-400`}
          >
            + Add connection expiry time
          </div>

          {expireOptions && (
            <div className="text-gray-800 dark:text-neutral-200">
              <p className="text-lg font-medium mb-2">Connection expiry time</p>
              {!isNew && (
                <p className="mb-2 text-gray-600 dark:text-gray-300 text-sm">
                  Currently expiring on: {expiresAt?.toString() || "Never"}
                </p>
              )}
              <div id="expiry-days" className="grid grid-cols-4 gap-2 text-xs">
                {Object.keys(expiryOptions).map((expiry) => {
                  return (
                    <div
                      key={expiry}
                      onClick={() => handleDaysChange(expiryOptions[expiry])}
                      className={`cursor-pointer rounded border-2 ${
                        days == expiryOptions[expiry]
                          ? "border-indigo-500 dark:border-indigo-400 text-indigo-500 bg-indigo-100 dark:bg-indigo-900"
                          : "border-gray-200 dark:border-gray-400"
                      } text-center py-4`}
                    >
                      {expiry}
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </>
      ) : (
        <>
          <p className="text-lg font-medium mb-2 text-gray-800 dark:text-neutral-200">
            Connection expiry time
          </p>
          <p className="text-gray-600 dark:text-gray-300 text-sm">
            {expiresAt ? expiresAt?.toString() : "This app will never expire"}
          </p>
        </>
      )}
    </div>
  );
};

export default Permissions;
